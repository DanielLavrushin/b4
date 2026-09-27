package moderation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	ActionApprove   = "approve"
	ActionReject    = "reject"
	ActionHide      = "hide"
	ActionRestore   = "restore"
	ActionWithdraw  = "withdraw"
	ActionReinstate = "reinstate"
	ActionBan       = "ban"
	ActionUnban     = "unban"
	ActionTrust     = "trust"
	ActionUntrust   = "untrust"
	ActionRemove    = "remove"
	ActionDismiss   = "dismiss"
	ActionResolve   = "resolve"
	ActionReopen    = "reopen"

	CodeNotFound          = "not_found"
	CodeInvalidTransition = "invalid_transition"
	CodeStale             = "stale"
	CodeAuthorBanned      = "author_banned"
	CodeSetWithdrawn      = "set_withdrawn"
	CodeReasonRequired    = "reason_required"
	CodeConfirmMismatch   = "confirm_mismatch"
	CodeOwnKey            = "own_key"
	CodeBuiltinKey        = "builtin_key"
	CodeBadKey            = "bad_key"
	CodeUnknownKey        = "unknown_key"
	CodeUnknownAction     = "unknown_action"
	CodeNotWithdrawn      = "not_withdrawn"
	CodeBatchInvalid      = "batch_invalid"
	CodeTooMany           = "too_many"
	CodeNothingToDo       = "nothing_to_do"
	CodeBadTag            = "bad_tag"

	MaxBatch       = 200
	MaxReasonRunes = 500
)

type Actor struct {
	Kind string
	Ref  string
	IP   string
}

type Builds interface {
	Request(trigger string)
}

type Service struct {
	Store       *store.Store
	Builds      Builds
	HubKeyID    string
	BuiltinKeys []string
	Now         func() time.Time
}

type Error struct {
	Code    string
	Message string
	Params  map[string]interface{}
	Items   []Item
}

func (e *Error) Error() string {
	return e.Message
}

func fail(code, message string, params map[string]interface{}) *Error {
	return &Error{Code: code, Message: message, Params: params}
}

func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	if errors.Is(err, store.ErrNotFound) {
		return CodeNotFound
	}
	return ""
}

type Result struct {
	Code    string
	Params  map[string]interface{}
	Notice  string
	Items   []Item
	BatchID string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) request(trigger string) {
	if s.Builds != nil {
		s.Builds.Request(trigger)
	}
}

func (s *Service) builtinKeys() []string {
	if s.BuiltinKeys != nil {
		return s.BuiltinKeys
	}
	return hubwire.BuiltinHubKeys
}

func CleanText(raw string, max int) string {
	text := strings.TrimSpace(raw)
	runes := []rune(text)
	if len(runes) > max {
		text = strings.TrimSpace(string(runes[:max]))
	}
	return text
}

func newBatchID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw[:])
}

func entry(a Actor, at time.Time, action, kind, id string, version int, reason string) store.AuditEntry {
	return store.AuditEntry{
		At:         at,
		Actor:      a.Kind,
		ActorRef:   a.Ref,
		ActorIP:    a.IP,
		Action:     action,
		TargetKind: kind,
		TargetID:   id,
		Version:    version,
		Reason:     reason,
	}
}

func ref(setID string, version int) string {
	return setID + "/" + strconv.Itoa(version)
}

func ValidKey(key string) bool {
	return hubdata.ValidKeyHMAC(key)
}
