// Package whatsapp implements channel.Adapter on top of whatsmeow.
//
// One Manager owns a shared whatsmeow sqlstore Container plus a per-tenant
// goroutine that owns its own *whatsmeow.Client. The Container persists all
// session state (Postgres tables prefixed `whatsmeow_`) so sessions survive
// gateway restarts, satisfying spec §4.7 WA-INV-3 / WA-INV-5.
//
// Demo recipient model (operator-ux §4.2 ambiguity resolved deliberately):
// each tenant configures one "operator recipient JID" in channel_bindings.
// If empty at pairing time, we default to the paired account's own JID
// (WhatsApp's "Message Yourself" chat) so the demo works end-to-end with a
// single phone. Real deployments override with the operator's actual WhatsApp
// contact.
package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx driver for whatsmeow's database/sql sqlstore
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/jessepcc/victoria/internal/channel"
	"github.com/jessepcc/victoria/internal/domain"
)

// SessionUpdate is fed to the Manager's status callback when a tenant's
// session changes state. Used by the gateway to flush the durable outbound
// queue on (re)connect and to mark sessions disconnected for alerting.
type SessionUpdate struct {
	TenantID string
	Status   domain.SessionStatus
	JID      string
}

// InboundDispatch is invoked when whatsmeow delivers a message addressed to
// one of our paired devices. The Manager normalizes the message into a
// channel.InboundMessage and the gateway is responsible for translating into
// the in-process InboundReply / signal envelope.
type InboundDispatch func(ctx context.Context, tenantID string, msg channel.InboundMessage) error

// Config is what New requires to bring up a Manager.
type Config struct {
	// PostgresDSN is the DSN of the shared Postgres database. The whatsmeow
	// store creates its own `whatsmeow_*` tables here at boot (Container.Upgrade).
	PostgresDSN string
	// OnSession is called whenever a tenant's session_status changes.
	OnSession func(SessionUpdate)
	// Inbound is called whenever an inbound message arrives.
	Inbound InboundDispatch
	// BindingForTenant lets SendOutbound enforce tenant binding policy such
	// as the A0 read-only outbound guard.
	BindingForTenant func(ctx context.Context, tenantID string) (domain.ChannelBinding, error)
	// AuditOutboundBlocked records hard guard refusals. It is intentionally
	// synchronous so a refused send and its audit event stay coupled.
	AuditOutboundBlocked func(ctx context.Context, tenantID, dstJID, bodyHash, callSite string) error
	// Logger is whatsmeow's logger. Pass waLog.Noop in tests.
	Logger waLog.Logger
}

// Manager owns the shared whatsmeow Container and per-tenant clients.
type Manager struct {
	cfg       Config
	container *sqlstore.Container

	// sessionCtx outlives any single HTTP request. whatsmeow's QR channel and
	// connection lifetime hang off this context — passing the per-request
	// ctx would tear down the WebSocket the moment /init returned.
	sessionCtx    context.Context
	sessionCancel context.CancelFunc

	mu      sync.Mutex
	clients map[string]*tenantClient // keyed by tenant_id
}

type OutboundSender interface {
	SendOutbound(ctx context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error)
}

type OutboundBlockAuditor interface {
	RecordOutboundBlocked(ctx context.Context, tenantID, dstJID, bodyHash, callSite string) error
}

type GuardedAdapter struct {
	sender  OutboundSender
	auditor OutboundBlockAuditor
}

func NewGuardedAdapter(sender OutboundSender, auditor OutboundBlockAuditor) *GuardedAdapter {
	return &GuardedAdapter{sender: sender, auditor: auditor}
}

func (g *GuardedAdapter) SendOutboundWithBinding(ctx context.Context, binding domain.ChannelBinding, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	if err := EnforceA0OutboundGuard(ctx, binding, msg.RecipientIdentifier, msg.BodyText, func(ctx context.Context, tenantID, dstJID, bodyHash, callSite string) error {
		if g.auditor == nil {
			return nil
		}
		return g.auditor.RecordOutboundBlocked(ctx, tenantID, dstJID, bodyHash, callSite)
	}); err != nil {
		return channel.DeliveryReceipt{}, err
	}
	return g.sender.SendOutbound(ctx, msg)
}

// tenantClient encapsulates one tenant's whatsmeow session.
type tenantClient struct {
	tenantID string
	manager  *Manager
	device   *store.Device
	cli      *whatsmeow.Client

	mu          sync.Mutex
	pairing     bool
	currentQR   string
	pairingDone chan struct{}
	status      domain.SessionStatus

	// recipient where outbound packets are sent. When empty we default to the
	// device's own account JID (self-chat).
	recipient types.JID

	// recentSent records the IDs of messages we just sent, so their echoes
	// (A0 self-chat delivers our own sends back as inbound events) can be
	// filtered by ID rather than by inspecting the operator-visible body.
	recentSent *sentIDs
}

// sentIDs is a bounded, concurrency-safe set of recently-sent message IDs. It
// backs echo detection: an inbound event whose ID we just sent is our own
// message coming back, even for plain-text self-chat where no packet tag can be
// embedded in the (operator-visible) body.
type sentIDs struct {
	mu    sync.Mutex
	ring  []string
	idx   int
	set   map[string]struct{}
	limit int
}

func newSentIDs(limit int) *sentIDs {
	return &sentIDs{ring: make([]string, limit), set: make(map[string]struct{}, limit), limit: limit}
}

func (s *sentIDs) add(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[id]; ok {
		return
	}
	if evicted := s.ring[s.idx]; evicted != "" {
		delete(s.set, evicted)
	}
	s.ring[s.idx] = id
	s.set[id] = struct{}{}
	s.idx = (s.idx + 1) % s.limit
}

func (s *sentIDs) has(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[id]
	return ok
}

// New builds a Manager and brings up the shared whatsmeow Container. Caller
// should invoke Restore(ctx) once after construction to reconnect any
// previously-paired tenants from the database.
func New(ctx context.Context, cfg Config) (*Manager, error) {
	if cfg.PostgresDSN == "" {
		return nil, errors.New("whatsapp: PostgresDSN required")
	}
	if cfg.Inbound == nil {
		return nil, errors.New("whatsapp: Inbound dispatcher required")
	}
	if cfg.Logger == nil {
		cfg.Logger = waLog.Noop
	}
	if cfg.OnSession == nil {
		cfg.OnSession = func(SessionUpdate) {}
	}
	// We use database/sql with pgx's stdlib driver so whatsmeow's sqlstore
	// can manage its own tables in the same Postgres database as Victoria.
	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: open db: %w", err)
	}
	container := sqlstore.NewWithDB(db, "pgx", cfg.Logger)
	if err := container.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("whatsapp: upgrade store: %w", err)
	}
	// whatsmeow ships with a hard-coded WhatsApp Web client version that goes
	// stale within weeks. Stale version → server immediately closes the
	// WebSocket with 405 → "couldn't link device" on the phone. Fetch the
	// current version live before any pairing attempt.
	if latest, err := whatsmeow.GetLatestVersion(ctx, http.DefaultClient); err == nil {
		store.SetWAVersion(*latest)
		cfg.Logger.Infof("whatsapp version refreshed: %s", latest.String())
	} else {
		cfg.Logger.Warnf("whatsapp version refresh failed (%v); falling back to library default", err)
	}
	sctx, scancel := context.WithCancel(context.Background())
	return &Manager{
		cfg:           cfg,
		container:     container,
		sessionCtx:    sctx,
		sessionCancel: scancel,
		clients:       map[string]*tenantClient{},
	}, nil
}

// Channel implements channel.Adapter.
func (m *Manager) Channel() channel.Channel { return channel.ChannelWhatsApp }

// SendOutbound implements channel.Adapter. The recipient identifier in the
// OutboundMessage is ignored — we use the tenant's configured recipient JID,
// resolved from the paired session.
func (m *Manager) SendOutbound(ctx context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	tc, ok := m.client(msg.TenantID)
	if !ok {
		return channel.DeliveryReceipt{}, channel.ErrAdapterNotAvailable
	}
	// Snapshot the session fields under the per-client lock: the whatsmeow event
	// handler mutates status/recipient on another goroutine (see publish and
	// handleEvent). Release the lock before any network call.
	tc.mu.Lock()
	cli := tc.cli
	status := tc.status
	to := tc.recipient
	device := tc.device
	tc.mu.Unlock()

	if cli == nil || !cli.IsConnected() || status != domain.SessionActive {
		return channel.DeliveryReceipt{}, channel.ErrSessionNotConnected
	}
	if msg.RecipientIdentifier != "" {
		jid, err := parseJID(msg.RecipientIdentifier)
		if err != nil {
			return channel.DeliveryReceipt{}, fmt.Errorf("whatsapp: bad recipient: %w", err)
		}
		to = jid
	}
	if m.cfg.BindingForTenant != nil {
		binding, err := m.cfg.BindingForTenant(ctx, msg.TenantID)
		if err != nil {
			return channel.DeliveryReceipt{}, err
		}
		if err := EnforceA0OutboundGuard(ctx, binding, to.String(), msg.BodyText, m.cfg.AuditOutboundBlocked); err != nil {
			return channel.DeliveryReceipt{}, err
		}
	}
	// WhatsApp's self-chat ("Message Yourself") silently drops interactive
	// List Messages. When the recipient is our own account JID, downgrade to
	// the numbered-text fallback that spec §4.6 documents.
	selfChat := device != nil && device.ID != nil && to.User == device.ID.User
	wamsg := buildOutboundMessage(msg, selfChat)
	resp, err := cli.SendMessage(ctx, to, wamsg, whatsmeow.SendRequestExtra{ID: msg.IdempotencyKey})
	if err != nil {
		return channel.DeliveryReceipt{}, fmt.Errorf("whatsapp: send: %w", err)
	}
	// Remember the ID so the self-chat echo of this send is filtered on inbound.
	tc.recentSent.add(resp.ID)
	return channel.DeliveryReceipt{ProviderMessageID: resp.ID, SentAt: resp.Timestamp}, nil
}

// NormalizeInboundWebhook is unused for WhatsApp (no webhook surface — events
// arrive via the whatsmeow socket). We satisfy the interface with a no-op so
// the same ChannelAdapter type can stand in for both backends.
func (*Manager) NormalizeInboundWebhook([]byte) ([]channel.InboundMessage, error) {
	return nil, nil
}

// BeginPairing starts (or restarts) a pairing flow for the given tenant.
// Returns the first QR code string and the pairing channel; the caller can
// poll WaitForPairing or read CurrentQR for subsequent QR rotations.
//
// Concurrency: the lock is held across the entire claim+install sequence so
// two concurrent callers cannot both create new clients and overwrite each
// other. Releasing the lock during the long-running Connect() avoids
// blocking other tenants.
func (m *Manager) BeginPairing(ctx context.Context, tenantID string) (string, error) {
	m.mu.Lock()
	if existing, ok := m.clients[tenantID]; ok {
		existing.mu.Lock()
		qr := existing.currentQR
		paired := existing.cli != nil && existing.cli.Store.ID != nil
		existing.mu.Unlock()
		if paired {
			m.mu.Unlock()
			return "", fmt.Errorf("whatsapp: tenant %s already paired", tenantID)
		}
		if qr != "" {
			m.mu.Unlock()
			return qr, nil
		}
	}

	device := m.container.NewDevice()
	tc := &tenantClient{
		tenantID:    tenantID,
		manager:     m,
		device:      device,
		status:      domain.SessionQRNeeded,
		pairingDone: make(chan struct{}),
		pairing:     true,
		recentSent:  newSentIDs(64),
	}
	tc.cli = whatsmeow.NewClient(device, m.cfg.Logger)
	tc.cli.AddEventHandler(tc.handleEvent)
	// Install eagerly so a concurrent BeginPairing for the same tenant sees us
	// and short-circuits on the existing-client check above.
	m.clients[tenantID] = tc
	m.mu.Unlock()

	// Use the manager's long-lived ctx — NOT the per-request ctx.
	// whatsmeow's qrChannel cancels the WebSocket when this ctx is done.
	qrChan, err := tc.cli.GetQRChannel(m.sessionCtx)
	if err != nil {
		m.mu.Lock()
		delete(m.clients, tenantID)
		m.mu.Unlock()
		return "", fmt.Errorf("whatsapp: get qr channel: %w", err)
	}
	if err := tc.cli.Connect(); err != nil {
		m.mu.Lock()
		delete(m.clients, tenantID)
		m.mu.Unlock()
		return "", fmt.Errorf("whatsapp: connect: %w", err)
	}

	// Pump QR events: the first QR is delivered synchronously below, subsequent
	// rotations land on currentQR for the GET /qr endpoint.
	firstQR := make(chan string, 1)
	go func() {
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				tc.mu.Lock()
				tc.currentQR = evt.Code
				tc.mu.Unlock()
				select {
				case firstQR <- evt.Code:
				default:
				}
			case "success":
				tc.mu.Lock()
				tc.currentQR = ""
				tc.pairing = false
				close(tc.pairingDone)
				tc.mu.Unlock()
			case "timeout", "err-client-outdated", "err-scanned-without-multidevice":
				tc.mu.Lock()
				tc.currentQR = ""
				tc.pairing = false
				select {
				case <-tc.pairingDone:
				default:
					close(tc.pairingDone)
				}
				tc.mu.Unlock()
				m.publish(tc, domain.SessionDisconnected)
				return
			}
		}
	}()

	select {
	case code := <-firstQR:
		m.publish(tc, domain.SessionQRNeeded)
		return code, nil
	case <-time.After(15 * time.Second):
		m.abortPairing(tenantID, tc)
		return "", errors.New("whatsapp: timed out waiting for first QR")
	case <-ctx.Done():
		m.abortPairing(tenantID, tc)
		return "", ctx.Err()
	}
}

// abortPairing tears down a pairing attempt that never produced a usable QR
// (manager-side timeout or caller cancellation). It removes the half-installed
// client so a later BeginPairing starts cleanly, and disconnects it to release
// the socket; the QR pump goroutine exits once whatsmeow closes qrChan.
func (m *Manager) abortPairing(tenantID string, tc *tenantClient) {
	m.mu.Lock()
	if m.clients[tenantID] == tc {
		delete(m.clients, tenantID)
	}
	m.mu.Unlock()
	if tc.cli != nil {
		tc.cli.Disconnect()
	}
}

// CurrentQR returns the latest QR code for an in-progress pairing, or empty
// if the tenant is fully paired or not currently pairing.
func (m *Manager) CurrentQR(tenantID string) string {
	tc, ok := m.client(tenantID)
	if !ok {
		return ""
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.currentQR
}

// Status returns the current session status for the given tenant.
func (m *Manager) Status(tenantID string) domain.SessionStatus {
	tc, ok := m.client(tenantID)
	if !ok {
		return domain.SessionUnknown
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.status
}

// Logout disconnects the tenant's session and removes the device from the
// shared store. After this the tenant must re-pair (BeginPairing).
func (m *Manager) Logout(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	tc, ok := m.clients[tenantID]
	if ok {
		delete(m.clients, tenantID)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if tc.cli != nil {
		_ = tc.cli.Logout(ctx)
		tc.cli.Disconnect()
	}
	if tc.device != nil && tc.device.ID != nil {
		_ = m.container.DeleteDevice(ctx, tc.device)
	}
	m.publish(tc, domain.SessionSuspended)
	return nil
}

// Restore reconnects every previously-paired device. Call once at boot.
func (m *Manager) Restore(ctx context.Context, bindings []domain.ChannelBinding) error {
	devices, err := m.container.GetAllDevices(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp: list devices: %w", err)
	}
	// Index by JID for quick lookup. Not every paired device necessarily has
	// a matching binding (operator may have re-provisioned); we silently skip
	// orphan devices since they'll be cleaned up via DeleteDevice elsewhere.
	for _, binding := range bindings {
		if binding.Channel != string(channel.ChannelWhatsApp) {
			continue
		}
		dev := pickDevice(devices, binding.ProviderNumber)
		if dev == nil {
			continue
		}
		tc := &tenantClient{
			tenantID:    binding.TenantID,
			manager:     m,
			device:      dev,
			status:      domain.SessionConnecting,
			pairingDone: closedChan(),
			recipient:   *dev.ID,
			recentSent:  newSentIDs(64),
		}
		tc.cli = whatsmeow.NewClient(dev, m.cfg.Logger)
		tc.cli.AddEventHandler(tc.handleEvent)
		if err := tc.cli.Connect(); err != nil {
			m.cfg.Logger.Warnf("restore: connect failed for %s: %v", binding.TenantID, err)
			continue
		}
		m.mu.Lock()
		m.clients[binding.TenantID] = tc
		m.mu.Unlock()
	}
	return nil
}

// Close tears down all sessions and the underlying database pool. Cancelling
// sessionCtx propagates to whatsmeow's QR emitters and read pumps.
func (m *Manager) Close() {
	if m.sessionCancel != nil {
		m.sessionCancel()
	}
	m.mu.Lock()
	clients := make([]*tenantClient, 0, len(m.clients))
	for _, tc := range m.clients {
		clients = append(clients, tc)
	}
	m.clients = map[string]*tenantClient{}
	m.mu.Unlock()
	for _, tc := range clients {
		if tc.cli != nil {
			tc.cli.Disconnect()
		}
	}
	_ = m.container.Close()
}

func (m *Manager) client(tenantID string) (*tenantClient, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tc, ok := m.clients[tenantID]
	return tc, ok
}

func (m *Manager) publish(tc *tenantClient, status domain.SessionStatus) {
	tc.mu.Lock()
	tc.status = status
	jid := ""
	if tc.device != nil && tc.device.ID != nil {
		jid = tc.device.ID.String()
	}
	tc.mu.Unlock()
	m.cfg.OnSession(SessionUpdate{TenantID: tc.tenantID, Status: status, JID: jid})
}

func (tc *tenantClient) handleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Connected:
		if tc.device != nil && tc.device.ID != nil {
			tc.mu.Lock()
			if (tc.recipient == types.JID{}) {
				tc.recipient = *tc.device.ID
			}
			tc.mu.Unlock()
		}
		tc.manager.publish(tc, domain.SessionActive)
	case *events.Disconnected:
		tc.manager.publish(tc, domain.SessionDisconnected)
	case *events.LoggedOut:
		tc.manager.publish(tc, domain.SessionSuspended)
	case *events.PairSuccess:
		tc.mu.Lock()
		tc.recipient = evt.ID
		tc.currentQR = ""
		tc.pairing = false
		tc.mu.Unlock()
	case *events.Message:
		tc.dispatchInbound(evt)
	}
}

func (tc *tenantClient) dispatchInbound(evt *events.Message) {
	if evt == nil || evt.Message == nil {
		return
	}
	body := extractText(evt.Message)
	buttonID := extractButtonReply(evt.Message)
	// Skip echoes of our own outbound. In A0 the operator shares their WhatsApp
	// account, so our sends arrive back as inbound events with IsFromMe=true.
	// Primary signal: the message ID we recorded when sending (works for
	// plain-text self-chat where no packet tag is embedded). Fallback: the
	// packet tag carried in rich outbound bodies.
	if tc.recentSent.has(evt.Info.ID) || (evt.Info.IsFromMe && strings.Contains(body, "[packet:")) {
		tc.manager.cfg.Logger.Debugf("skipping outbound echo for tenant %s msg=%s", tc.tenantID, evt.Info.ID)
		return
	}
	// Keep PII (sender JID, message body) out of Info-level logs — it includes
	// non-allowlisted senders. Log only non-identifying metadata at Info; the
	// raw content is available at Debug for local troubleshooting.
	tc.manager.cfg.Logger.Infof("inbound tenant=%s msg=%s button=%q text_len=%d", tc.tenantID, evt.Info.ID, buttonID, len(body))
	tc.manager.cfg.Logger.Debugf("inbound detail tenant=%s sender=%s text=%q", tc.tenantID, evt.Info.Sender, body)
	in := channel.InboundMessage{
		SenderIdentifier:  evt.Info.Sender.User,
		SenderJID:         evt.Info.Sender.String(),
		ProviderMessageID: evt.Info.ID,
		Channel:           channel.ChannelWhatsApp,
		ButtonPayload:     buttonID,
		FreeText:          body,
		ReceivedAt:        evt.Info.Timestamp,
		IsFromMe:          evt.Info.IsFromMe,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tc.manager.cfg.Inbound(ctx, tc.tenantID, in); err != nil {
		tc.manager.cfg.Logger.Warnf("inbound dispatch failed for %s: %v", tc.tenantID, err)
	}
}

func extractText(m *waProto.Message) string {
	if m == nil {
		return ""
	}
	if m.Conversation != nil && *m.Conversation != "" {
		return *m.Conversation
	}
	if m.ExtendedTextMessage != nil && m.ExtendedTextMessage.Text != nil {
		return *m.ExtendedTextMessage.Text
	}
	return ""
}

func extractButtonReply(m *waProto.Message) string {
	if m == nil {
		return ""
	}
	if r := m.ButtonsResponseMessage; r != nil && r.SelectedButtonID != nil {
		return *r.SelectedButtonID
	}
	if r := m.ListResponseMessage; r != nil && r.SingleSelectReply != nil && r.SingleSelectReply.SelectedRowID != nil {
		return *r.SingleSelectReply.SelectedRowID
	}
	if r := m.TemplateButtonReplyMessage; r != nil && r.SelectedID != nil {
		return *r.SelectedID
	}
	return ""
}

func parseJID(input string) (types.JID, error) {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return types.JID{}, errors.New("empty jid")
	}
	if strings.ContainsRune(clean, '@') {
		return types.ParseJID(clean)
	}
	clean = strings.TrimPrefix(clean, "+")
	return types.NewJID(clean, types.DefaultUserServer), nil
}

func normalizeJIDString(input string) string {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return ""
	}
	if strings.ContainsRune(clean, '@') {
		return clean
	}
	clean = strings.TrimPrefix(clean, "+")
	return clean + "@" + types.DefaultUserServer
}

// EnforceA0OutboundGuard refuses any A0 (read_only) outbound that isn't
// destined for the operator's own JID OR their configured draft-delivery JID
// (per spec OQ-2 RESOLVED). Any block writes outbound_blocked_to_customer via
// the audit callback.
func EnforceA0OutboundGuard(ctx context.Context, binding domain.ChannelBinding, recipient, body string, audit func(context.Context, string, string, string, string) error) error {
	if binding.InboundMode != domain.InboundModeReadOnly {
		return nil
	}
	dst := normalizeJIDString(recipient)
	if dst == "" {
		return nil
	}
	operator := normalizeJIDString(binding.OperatorJID)
	if operator == "" {
		operator = normalizeJIDString(binding.ProviderNumber)
	}
	draftJID := normalizeJIDString(binding.DraftDeliveryJID)
	if dst == operator || (draftJID != "" && dst == draftJID) {
		return nil
	}
	bodyHash := domain.SHA256Key(body)
	if audit != nil {
		_ = audit(ctx, binding.TenantID, dst, bodyHash, string(debug.Stack()))
	}
	return fmt.Errorf("whatsapp: outbound blocked to customer in read_only mode")
}

func pickDevice(devices []*store.Device, providerNumber string) *store.Device {
	target := strings.TrimPrefix(strings.TrimSpace(providerNumber), "+")
	for _, d := range devices {
		if d.ID == nil {
			continue
		}
		if target == "" || d.ID.User == target {
			return d
		}
	}
	return nil
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// buildOutboundMessage renders a packet as a WhatsApp List Message when the
// packet has tappable buttons (operator-ux §4.6 — pinned WhatsApp rendering).
// Falls back to plain text when there are no buttons OR when the recipient
// is our own account (self-chat doesn't render interactive messages).
//
// The packet tag `[packet:<id>]` is embedded in the description so inbound
// reply parsing can route the operator's selection back to the right packet.
func buildOutboundMessage(msg channel.OutboundMessage, forceText bool) *waProto.Message {
	if len(msg.Buttons) == 0 || forceText {
		return &waProto.Message{Conversation: proto.String(renderText(msg))}
	}
	rows := make([]*waProto.ListMessage_Row, 0, len(msg.Buttons))
	for _, btn := range msg.Buttons {
		rows = append(rows, &waProto.ListMessage_Row{
			RowID: proto.String(btn.ID),
			Title: proto.String(btn.Label),
		})
	}
	listType := waProto.ListMessage_SINGLE_SELECT
	listMsg := &waProto.ListMessage{
		Title:       proto.String("Victoria — review needed"),
		Description: proto.String(msg.BodyText + "\n\n[packet:" + msg.PacketID + "]"),
		ButtonText:  proto.String("Choose action"),
		FooterText:  proto.String("Reply or tap an option"),
		ListType:    &listType,
		Sections: []*waProto.ListMessage_Section{{
			Title: proto.String("Available actions"),
			Rows:  rows,
		}},
	}
	return &waProto.Message{ListMessage: listMsg}
}

// renderText is the fallback when no buttons are present (or for clients that
// can't render List Messages). Just the packet body — no numbered list, no
// visible packet-id tag. Reply routing falls back to LatestReviewPacket per
// tenant, which is correct for the single-active-packet case.
func renderText(msg channel.OutboundMessage) string {
	return msg.BodyText
}

// ButtonForReply maps a numeric reply or text reply to one of the buttons in
// the supplied set. Returns "" if no match. Shared with the gateway parser.
func ButtonForReply(reply string, buttons []channel.ButtonSpec) string {
	clean := strings.TrimSpace(reply)
	if clean == "" {
		return ""
	}
	if n, err := atoi(clean); err == nil && n >= 1 && n <= len(buttons) {
		return buttons[n-1].ID
	}
	lower := strings.ToLower(clean)
	for _, b := range buttons {
		if strings.EqualFold(b.ID, clean) || strings.EqualFold(b.Label, clean) {
			return b.ID
		}
		if strings.HasPrefix(lower, strings.ToLower(b.Label)) {
			return b.ID
		}
	}
	return ""
}

func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
