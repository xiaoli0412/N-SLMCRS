package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nslmcrs/gateway/internal/config"
	"github.com/nslmcrs/gateway/internal/data"
)

// newTestHandler 构建一个绑定临时 SQLite 存储的管理 Handler + gin 引擎。
func newTestHandler(t *testing.T) (*gin.Engine, *data.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := data.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("data.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	cfg := &config.Config{
		Server: config.ServerConfig{Port: 8787, AdminToken: config.DefaultAdminToken},
	}
	h := New(store, nil, cfg)
	r := gin.New()
	h.RegisterRoutes(r)
	return r, store
}

func doJSON(t *testing.T, r http.Handler, method, path string, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return m
}

func TestAuthStatus_InitialState(t *testing.T) {
	r, _ := newTestHandler(t)
	w := doJSON(t, r, "GET", "/api/admin/auth/status", "", nil)
	if w.Code != 200 {
		t.Fatalf("auth/status code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	m := decodeBody(t, w)
	if m["must_change_password"] != true {
		t.Errorf("初始状态 must_change_password = %v, want true", m["must_change_password"])
	}
	if m["initialized"] != false {
		t.Errorf("初始状态 initialized = %v, want false", m["initialized"])
	}
}

func TestLogin_DefaultToken(t *testing.T) {
	r, _ := newTestHandler(t)
	// 正确默认令牌
	w := doJSON(t, r, "POST", "/api/admin/login", "", map[string]string{"token": config.DefaultAdminToken})
	if w.Code != 200 {
		t.Fatalf("login(ADMIN) code=%d body=%s", w.Code, w.Body.String())
	}
	m := decodeBody(t, w)
	if m["ok"] != true {
		t.Errorf("login ok = %v, want true", m["ok"])
	}
	if m["must_change_password"] != true {
		t.Errorf("首次登录 must_change_password = %v, want true", m["must_change_password"])
	}
	// 错误令牌
	w = doJSON(t, r, "POST", "/api/admin/login", "", map[string]string{"token": "wrong"})
	if w.Code != 401 {
		t.Errorf("login(wrong) code=%d, want 401", w.Code)
	}
}

func TestChangePassword_FullFlow(t *testing.T) {
	r, store := newTestHandler(t)
	def := config.DefaultAdminToken

	// 受保护端点：默认令牌在改密前被锁定 → 403 + must_change_password
	w := doJSON(t, r, "GET", "/api/admin/keys", def, nil)
	if w.Code != 403 {
		t.Fatalf("默认令牌访问 keys 应被锁定 code=%d body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Admin-Must-Change") != "1" {
		t.Error("锁定响应头应带 X-Admin-Must-Change: 1")
	}
	// 错误令牌被拒（401）
	w = doJSON(t, r, "GET", "/api/admin/keys", "nope", nil)
	if w.Code != 401 {
		t.Errorf("错误令牌 code=%d, want 401", w.Code)
	}

	// 改密：拒绝过短
	w = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": def, "next": "123"})
	if w.Code != 400 {
		t.Errorf("过短新令牌 code=%d, want 400", w.Code)
	}
	// 改密：拒绝 ADMIN（默认值，大小写无关）
	w = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": def, "next": "admin"})
	if w.Code != 400 {
		t.Errorf("新令牌=admin(默认) code=%d, want 400", w.Code)
	}
	w = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": def, "next": "ADMIN"})
	if w.Code != 400 {
		t.Errorf("新令牌=ADMIN(默认) code=%d, want 400", w.Code)
	}
	// 改密：current 错误
	w = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": "bad", "next": "newsecret123"})
	if w.Code != 401 {
		t.Errorf("current 错误 code=%d, want 401", w.Code)
	}
	// 改密：成功
	w = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": def, "next": "newsecret123"})
	if w.Code != 200 {
		t.Fatalf("change-password code=%d body=%s", w.Code, w.Body.String())
	}

	// 哈希已落库
	hash, _ := store.GetSetting(t.Context(), "admin:token_hash")
	if hash == "" {
		t.Fatal("改密后 admin:token_hash 应已写入")
	}

	// 旧默认令牌失效（401）
	w = doJSON(t, r, "GET", "/api/admin/keys", def, nil)
	if w.Code != 401 {
		t.Errorf("改密后旧默认令牌 code=%d, want 401", w.Code)
	}
	// 新令牌生效，且不再锁定（200）
	w = doJSON(t, r, "GET", "/api/admin/keys", "newsecret123", nil)
	if w.Code != 200 {
		t.Errorf("新令牌 code=%d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Admin-Must-Change"); got != "" {
		t.Errorf("改密后不应再带 must-change 头, got %q", got)
	}
	// login 也不再要求改密
	w = doJSON(t, r, "POST", "/api/admin/login", "", map[string]string{"token": "newsecret123"})
	m := decodeBody(t, w)
	if m["must_change_password"] != false {
		t.Errorf("改密后 login must_change_password = %v, want false", m["must_change_password"])
	}
}

func TestAuthStatus_AfterChange(t *testing.T) {
	r, _ := newTestHandler(t)
	_ = doJSON(t, r, "POST", "/api/admin/change-password", "", map[string]string{"current": config.DefaultAdminToken, "next": "anotherpw1"})
	w := doJSON(t, r, "GET", "/api/admin/auth/status", "", nil)
	m := decodeBody(t, w)
	if m["must_change_password"] != false {
		t.Errorf("改密后 auth/status must_change_password = %v, want false", m["must_change_password"])
	}
	if m["initialized"] != true {
		t.Errorf("改密后 initialized = %v, want true", m["initialized"])
	}
}
