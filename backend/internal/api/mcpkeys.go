package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"vigil/internal/mcp"
	"vigil/internal/models"
	"vigil/internal/store"
)

// ---- MCP API Key 管理 ----

// MCP Key 明文格式：`vigil_mcp_` + 64 位十六进制随机串（32 字节熵）。
// 前缀让密钥在客户端配置里可辨认，也便于在泄漏扫描中识别。
const mcpKeySalt = "vigil_mcp_"

// mcpKeyDTO 对外形态：绝不回传密钥本体（明文只在创建响应中出现一次）。
type mcpKeyDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	KeyPrefix  string `json:"key_prefix"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
}

func mcpKeyToDTO(k *models.MCPKey) mcpKeyDTO {
	dto := mcpKeyDTO{ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, CreatedAt: k.CreatedAt.UTC().Format("2006-01-02 15:04:05")}
	if k.LastUsedAt != nil {
		dto.LastUsedAt = k.LastUsedAt.UTC().Format("2006-01-02 15:04:05")
	}
	return dto
}

// generateMCPKey 生成新密钥，返回 (明文, 前缀展示片段, 哈希)。
func generateMCPKey() (full, prefix, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", err
	}
	full = mcpKeySalt + hex.EncodeToString(b)
	prefix = full[:len(mcpKeySalt)+8] + "…"
	hash = mcp.HashAPIKey(full)
	return full, prefix, hash, nil
}

// handleMCPKeys 提供密钥列表（GET）与生成（POST）。
// POST 响应中的 key 明文是唯一一次展示机会，前端须提示用户立即保存。
func (a *api) handleMCPKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := a.store.ListMCPKeys()
		if err != nil {
			serverError(w, r, err)
			return
		}
		out := make([]mcpKeyDTO, 0, len(keys))
		for i := range keys {
			out = append(out, mcpKeyToDTO(&keys[i]))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"keys": out, "count": len(out)})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		name := strings.TrimSpace(body.Name)
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请填写密钥名称"})
			return
		}
		if len(name) > 50 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "名称过长（最多 50 字符）"})
			return
		}
		full, prefix, hash, err := generateMCPKey()
		if err != nil {
			serverError(w, r, err)
			return
		}
		created, err := a.store.CreateMCPKey(name, prefix, hash)
		if err != nil {
			serverError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"key":    full, // 仅此一次返回明文
			"record": mcpKeyToDTO(created),
		})
	default:
		methodNotAllowed(w)
	}
}

// handleMCPKeyByID 提供单条密钥的删除（吊销）。吊销后使用该 Key 的
// MCP 客户端立即失效（每次请求都查库校验）。
func (a *api) handleMCPKeyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mcp/keys/"), "/"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err := a.store.DeleteMCPKey(id); err != nil {
		if err == store.ErrMCPKeyNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "密钥不存在"})
			return
		}
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
}
