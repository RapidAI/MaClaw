package cloudagent

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// SecretOK compares the admin secret in constant time. An empty secret never matches.
func SecretOK(provided, secret string) bool {
	if secret == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1
}

func (rt *Runtime) ServePut(w http.ResponseWriter, r *http.Request) {
	var spec Spec
	if !decodeJSON(w, r, &spec) {
		return
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		if spec.ID != "" && spec.ID != id {
			writeErr(w, http.StatusBadRequest, "实例标识不匹配")
			return
		}
		spec.ID = id
	}
	if err := rt.Put(spec); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": spec.ID})
}

func (rt *Runtime) ServeControl(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Op          string          `json:"op"`
		Body        json.RawMessage `json:"body"`
		OwnerUserID string          `json:"owner_user_id"`
		GrantID     string          `json:"grant_id"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	id := r.PathValue("id")
	if payload.Op == "cloud_unwatch_task" {
		rt.dropGrant(id, payload.GrantID)
	} else {
		rt.noteGrant(id, payload.GrantID)
	}
	raw, err := rt.Control(r.Context(), id, payload.OwnerUserID, payload.Op, payload.Body)
	if err != nil {
		writeRuntimeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (rt *Runtime) ServeStop(w http.ResponseWriter, r *http.Request) {
	if err := rt.Stop(r.PathValue("id")); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (rt *Runtime) ServeResume(w http.ResponseWriter, r *http.Request) {
	if _, err := rt.instance(r.PathValue("id")); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (rt *Runtime) ServeDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := rt.Stop(id); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	if err := rt.Delete(id); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (rt *Runtime) ServeLLM(w http.ResponseWriter, r *http.Request) {
	var in CustomLLM
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := rt.SaveLLM(r.PathValue("id"), in); err != nil {
		writeRuntimeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest); err != nil {
		writeErr(w, http.StatusBadRequest, "请求无效")
		return false
	}
	return true
}

func writeRuntimeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, errOwner), errors.Is(err, errKind), errors.Is(err, errSessionGit):
		writeErr(w, http.StatusForbidden, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}

func writeErr(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "云端管理暂时不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
