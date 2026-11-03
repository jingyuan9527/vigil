package mcp

import (
	"errors"
	"fmt"
	"strings"

	"vigil/internal/models"
	"vigil/internal/store"
)

// Tool 是一个可被 MCP 客户端调用的工具：名称、描述、JSON Schema 入参与处理函数。
// Handler 返回的任意值会被序列化为 JSON 文本内容回给客户端。
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args map[string]any) (any, error)
}

// Server 持有存储层引用与工具注册表。数据直接读 store（与 Web 服务同一 SQLite 库），
// 不经过 HTTP，也不引入 scanner/registry 依赖——MCP 形态只做查看与标记。
type Server struct {
	store *store.Store
	tools []Tool
}

// NewServer 构造 Server：stdio 与 HTTP 传输共用。
func NewServer(st *store.Store) *Server {
	return &Server{store: st, tools: buildTools(st)}
}

// tool 按名称查找工具，未注册返回 nil。
func (s *Server) tool(name string) *Tool {
	for i := range s.tools {
		if s.tools[i].Name == name {
			return &s.tools[i]
		}
	}
	return nil
}

// toolDescriptors 输出 tools/list 所需的工具描述（不含 Handler）。
func (s *Server) toolDescriptors() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return out
}

// buildTools 构造全部工具：4 个只读 + 3 个标记，均落在 store 现有能力上。
func buildTools(st *store.Store) []Tool {
	return []Tool{
		{
			Name:        "list_images",
			Description: "列出 Vigil 监控的全部 Docker 镜像及其更新状态。status 可选过滤：up-to-date（已最新）/ update-available（有更新）/ stale（本机已删除）/ unknown（未知）。",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"status": map[string]any{"type": "string", "description": "按更新状态过滤，缺省返回全部"}},
			},
			Handler: func(args map[string]any) (any, error) {
				status := argString(args, "status")
				switch models.ImageStatus(status) {
				case "", models.StatusUnknown, models.StatusUpToDate, models.StatusUpdateAvailable, models.StatusStale:
				default:
					return nil, fmt.Errorf("无效的 status %q，可选值：unknown / up-to-date / update-available / stale", status)
				}
				imgs, err := st.ListImages(status)
				if err != nil {
					return nil, err
				}
				if imgs == nil {
					imgs = []models.Image{}
				}
				return map[string]any{"count": len(imgs), "images": imgs}, nil
			},
		},
		{
			Name:        "get_image",
			Description: "查询单个镜像详情：摘要对比、检测模式与版本时间线（最近 50 次扫描记录的摘要快照）。id 可从 list_images 结果获得。",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "镜像 ID"},
				},
			},
			Handler: func(args map[string]any) (any, error) {
				id, ok := argInt64(args, "id")
				if !ok {
					return nil, errors.New("缺少必填参数 id")
				}
				img, err := st.GetImage(id)
				if err != nil {
					return nil, err
				}
				if img == nil {
					return nil, fmt.Errorf("镜像不存在: id=%d", id)
				}
				versions, err := st.ListVersions(id)
				if err != nil {
					// 时间线查询失败只降级（与 Web 详情接口同策略）
					versions = []models.ImageVersion{}
				}
				return models.ImageDetail{Image: *img, Versions: versions}, nil
			},
		},
		{
			Name:        "list_notifications",
			Description: "列出镜像更新通知（每页最多 100 条，按时间倒序）。unread_only=true 只看未读；翻页时把上一页结果里的 next_cursor 传给 cursor。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"unread_only": map[string]any{"type": "boolean", "description": "仅列出未读通知"},
					"cursor":      map[string]any{"type": "integer", "description": "分页游标（上一页的 next_cursor）"},
				},
			},
			Handler: func(args map[string]any) (any, error) {
				unread := argBool(args, "unread_only")
				cursor, _ := argInt64(args, "cursor")
				list, err := st.ListNotifications(unread, cursor)
				if err != nil {
					return nil, err
				}
				if list == nil {
					list = []models.Notification{}
				}
				out := map[string]any{"count": len(list), "notifications": list}
				if len(list) == 100 {
					out["next_cursor"] = list[len(list)-1].ID
				}
				return out, nil
			},
		},
		{
			Name:        "get_stats",
			Description: "获取 Vigil 仪表盘统计：镜像总数及各状态分布、未读通知数、最近一次扫描时间与结果。",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Handler: func(args map[string]any) (any, error) {
				return st.Stats()
			},
		},
		{
			Name:        "add_ignore",
			Description: "忽略某个镜像的更新提醒：之后扫描仍会检查但不再产生通知（行数据冻结）。提供 id 或 reference 任一即可；忽略状态也可用 remove_ignore 撤销。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":        map[string]any{"type": "integer", "description": "镜像 ID（与 reference 二选一）"},
					"reference": map[string]any{"type": "string", "description": "完整镜像引用，如 nginx:1.27（与 id 二选一）"},
				},
			},
			Handler: func(args map[string]any) (any, error) {
				return setIgnored(st, args, true)
			},
		},
		{
			Name:        "remove_ignore",
			Description: "取消忽略某个镜像：恢复其更新检测与通知。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":        map[string]any{"type": "integer", "description": "镜像 ID（与 reference 二选一）"},
					"reference": map[string]any{"type": "string", "description": "完整镜像引用（与 id 二选一）"},
				},
			},
			Handler: func(args map[string]any) (any, error) {
				return setIgnored(st, args, false)
			},
		},
		{
			Name:        "mark_notifications_read",
			Description: "把更新通知标记为已读。提供 id 时只标记该条；不带参数则全部未读通知标记为已读。",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "通知 ID；缺省时标记全部未读"},
				},
			},
			Handler: func(args map[string]any) (any, error) {
				if id, ok := argInt64(args, "id"); ok {
					if err := st.MarkRead(id); err != nil {
						return nil, err
					}
					return map[string]any{"result": "ok", "marked": "single", "id": id}, nil
				}
				if err := st.MarkAllRead(); err != nil {
					return nil, err
				}
				return map[string]any{"result": "ok", "marked": "all"}, nil
			},
		},
	}
}

// setIgnored 是 add_ignore / remove_ignore 的共用实现：优先按 id 定位，
// 缺 id 时回退到 reference（对模型更自然，无需先查一次列表）。
func setIgnored(st *store.Store, args map[string]any, ignored bool) (any, error) {
	var id int64
	if v, ok := argInt64(args, "id"); ok {
		id = v
	} else {
		ref := strings.TrimSpace(argString(args, "reference"))
		if ref == "" {
			return nil, errors.New("需要提供 id 或 reference")
		}
		img, err := st.GetImageByRef(ref)
		if err != nil {
			return nil, err
		}
		if img == nil {
			return nil, fmt.Errorf("镜像不存在: %s", ref)
		}
		id = img.ID
	}
	if err := st.SetIgnored(id, ignored); err != nil {
		return nil, err
	}
	img, err := st.GetImage(id)
	if err != nil {
		return nil, err
	}
	if img == nil {
		return nil, fmt.Errorf("镜像不存在: id=%d", id)
	}
	return map[string]any{"id": id, "reference": img.Reference, "ignored": img.Ignored}, nil
}

// ---- 参数提取辅助 ----

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

func argBool(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	b, _ := args[key].(bool)
	return b
}

// argInt64 宽容处理 JSON 数值：客户端可能传 1.0 这类浮点形态的整数。
func argInt64(args map[string]any, key string) (int64, bool) {
	if args == nil {
		return 0, false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case string:
		var i int64
		if _, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}
