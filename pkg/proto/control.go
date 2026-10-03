package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ControlRequest is a control request in either direction:
// {"type":"control_request","request_id":"…","request":{"subtype":"…",…}}.
type ControlRequest struct {
	Envelope
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
}

// RequestSubtype returns request.subtype.
func (c *ControlRequest) RequestSubtype() string {
	var s struct {
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(c.Request, &s)
	return s.Subtype
}

// DecodeRequest returns the typed payload of a CLI → client request (*CanUseTool,
// *HookCallback, *MCPMessage, *Elicitation, *RequestUserDialog) or, for other
// subtypes, a *GenericRequest. Shape mismatches are tolerated as in Decode.
func (c *ControlRequest) DecodeRequest() (any, error) {
	var v any
	switch c.RequestSubtype() {
	case SubCanUseTool:
		v = &CanUseTool{}
	case SubHookCallback:
		v = &HookCallback{}
	case SubMCPMessage:
		v = &MCPMessage{}
	case SubElicitation:
		v = &Elicitation{}
	case SubRequestUserDialog:
		v = &RequestUserDialog{}
	default:
		v = &GenericRequest{}
	}
	err := json.Unmarshal(c.Request, v)
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		err = nil
	}
	return v, err
}

// GenericRequest is a CLI → client request this package has no type for.
type GenericRequest struct {
	Subtype string `json:"subtype"`
}

// ControlResponse answers a control request in either direction.
type ControlResponse struct {
	Envelope
	Response ControlResponseBody `json:"response"`
}

// ControlResponseBody is {"subtype":"success"|"error","request_id",…}.
type ControlResponseBody struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response,omitempty"`
	Error     string          `json:"error,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`

	// initialize only: prompts that were pending before this client attached.
	PendingPermissionRequests json.RawMessage `json:"pending_permission_requests,omitempty"`
	PendingUserDialogRequests json.RawMessage `json:"pending_user_dialog_requests,omitempty"`
}

// Control response subtypes.
const (
	ResponseSuccess = "success"
	ResponseError   = "error"
)

// Err returns the response as an error, or nil for success.
func (b *ControlResponseBody) Err() error {
	if b.Subtype != ResponseError {
		return nil
	}
	return &ControlError{RequestID: b.RequestID, Message: b.Error, Code: b.ErrorCode}
}

// Decode unmarshals a success response payload into v.
func (b *ControlResponseBody) Decode(v any) error {
	if err := b.Err(); err != nil {
		return err
	}
	if len(b.Response) == 0 {
		return nil
	}
	err := json.Unmarshal(b.Response, v)
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return nil
	}
	return err
}

// ControlError is an error response to a control request.
type ControlError struct {
	RequestID string
	Message   string
	Code      string
}

func (e *ControlError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("control request %s: %s (%s)", e.RequestID, e.Message, e.Code)
	}
	return fmt.Sprintf("control request %s: %s", e.RequestID, e.Message)
}

// Unsupported reports whether the engine rejected the request's subtype as unknown.
func (e *ControlError) Unsupported() bool {
	return bytes.HasPrefix([]byte(e.Message), []byte("Unsupported control request subtype"))
}

// IsUnsupported reports whether err is a ControlError for an unsupported subtype.
func IsUnsupported(err error) bool {
	var ce *ControlError
	return errors.As(err, &ce) && ce.Unsupported()
}

// ControlCancelRequest withdraws an earlier control request (no reply).
type ControlCancelRequest struct {
	Envelope
	RequestID string `json:"request_id"`
}

// Request is the payload of a client → CLI control request. MarshalControlRequest adds
// the subtype.
type Request interface {
	ControlSubtype() string
}

// MarshalControlRequest encodes a control_request line (without the trailing newline).
func MarshalControlRequest(requestID string, req Request) ([]byte, error) {
	body, err := marshalWithSubtype(req.ControlSubtype(), req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		Request   json.RawMessage `json:"request"`
	}{TypeControlRequest, requestID, body})
}

// MarshalControlSuccess encodes a success control_response to a CLI request. A nil
// payload sends no "response" field.
func MarshalControlSuccess(requestID string, payload any) ([]byte, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(ControlResponse{
		Envelope: Envelope{Type: TypeControlResponse},
		Response: ControlResponseBody{Subtype: ResponseSuccess, RequestID: requestID, Response: raw},
	})
}

// MarshalControlError encodes an error control_response to a CLI request.
func MarshalControlError(requestID, message string) ([]byte, error) {
	return json.Marshal(ControlResponse{
		Envelope: Envelope{Type: TypeControlResponse},
		Response: ControlResponseBody{Subtype: ResponseError, RequestID: requestID, Error: message},
	})
}

// MarshalControlCancel encodes a control_cancel_request for one of our own requests.
func MarshalControlCancel(requestID string) ([]byte, error) {
	return json.Marshal(ControlCancelRequest{Envelope: Envelope{Type: TypeControlCancelRequest}, RequestID: requestID})
}

// marshalWithSubtype marshals v (which must encode as a JSON object) with "subtype"
// as its first key.
func marshalWithSubtype(subtype string, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		b = []byte("{}")
	}
	if len(b) < 2 || b[0] != '{' {
		return nil, fmt.Errorf("proto: %s request must encode as a JSON object", subtype)
	}
	st, _ := json.Marshal(subtype)
	var buf bytes.Buffer
	buf.WriteString(`{"subtype":`)
	buf.Write(st)
	if rest := bytes.TrimSpace(b[1:]); len(rest) > 0 && rest[0] != '}' {
		buf.WriteByte(',')
	}
	buf.Write(b[1:])
	return buf.Bytes(), nil
}
