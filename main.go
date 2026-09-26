package main

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/teddyli18000/mimo-web-proxy/internal/config"
	"github.com/teddyli18000/mimo-web-proxy/internal/convstore"
	"github.com/teddyli18000/mimo-web-proxy/internal/handler"
	"github.com/teddyli18000/mimo-web-proxy/internal/pool"
	"github.com/teddyli18000/mimo-web-proxy/internal/stats"
)

//go:embed static/*
var staticFiles embed.FS

// placeholderMarker 占位前端的特征串。
// static/ 不入库（由 web/ 构建产物填充），本地编译时如果没有先构建前端，
// 就会把这些占位文件嵌进二进制——面板看起来"能打开"但其实是空壳。
// 这种情况必须在启动日志里喊出来，否则会被静默交付出去。
const placeholderMarker = "dev placeholder"

// warnIfPlaceholderFrontend 检测嵌入的是否为占位前端
func warnIfPlaceholderFrontend(staticFS fs.FS) {
	data, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		log.Printf("⚠️  未嵌入前端页面（static/index.html 缺失），管理面板不可用。")
		log.Printf("    构建前请先执行：cd web && npm install && npm run build")
		return
	}
	if bytes.Contains(data, []byte(placeholderMarker)) {
		log.Printf("⚠️  当前二进制嵌入的是【占位前端】，管理面板只会显示提示页。")
		log.Printf("    这是本地编译未构建前端导致的；请先执行：cd web && npm install && npm run build")
		log.Printf("    官方发布产物不受影响（CI 会先构建前端）。")
	}
}

func main() {
	baseDir := resolveBaseDir()

	configPath := filepath.Join(baseDir, "config.json")
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		configPath = p
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化统计追踪
	dataDir := filepath.Join(baseDir, "data")
	os.MkdirAll(dataDir, 0755)
	stats.Init(filepath.Join(dataDir, "stats.json"))

	// 初始化账号池
	accountPool := pool.New(cfg.Accounts)

	// 路由
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	// 注意：不使用 middleware.RealIP —— 本服务直连部署，信任 X-Forwarded-For
	// 会被伪造头绕过来源校验。
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// 会话映射落盘：重启后仍能复用上游会话（否则首轮要重发完整上下文）
	convStore := convstore.NewPersisted(filepath.Join(dataDir, "conversations.json"))
	chatHandler := handler.NewChatHandler(accountPool, convStore)
	messagesHandler := handler.NewMessagesHandler(accountPool, convStore)
	adminHandler := handler.NewAdminHandler(accountPool)

	// OpenAI 兼容
	r.Route("/v1", func(r chi.Router) {
		r.Use(apiKeyAuth)
		r.Post("/chat/completions", chatHandler.Handle)
		r.Get("/models", handler.ModelsHandler)
		r.Get("/models/{id}", handler.ModelsHandler)
		r.Post("/messages", messagesHandler.Handle)
		r.Post("/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"input_tokens":0}`))
		})
	})

	// 无 /v1 前缀的别名：兼容把 Base URL 配成 http://localhost:8080
	// 而 client 自己拼 /chat/completions 的情况（如 DSH openai-completions 协议）
	r.With(apiKeyAuth).Post("/chat/completions", chatHandler.Handle)
	r.With(apiKeyAuth).Post("/completions", chatHandler.Handle)

	// Anthropic 快捷路径
	r.With(apiKeyAuth).Post("/anthropic/v1/messages", messagesHandler.Handle)

	// 管理接口（同样需要 API Key —— 防止局域网/恶意网页读取 cookie 凭证）
	r.Route("/admin/api", func(r chi.Router) {
		r.Use(apiKeyAuth)
		r.Get("/config", adminHandler.GetConfig)
		r.Post("/config", adminHandler.UpdateConfig)
		r.Post("/accounts", adminHandler.AddAccount)
		r.Delete("/accounts", adminHandler.DeleteAccount)
		r.Get("/health", adminHandler.HealthCheck)
		r.Get("/stats", adminHandler.GetStats)
	})

	// 前端 SPA
	staticFS, _ := fs.Sub(staticFiles, "static")
	warnIfPlaceholderFrontend(staticFS)
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		filePath := path[1:]
		if data, err := fs.ReadFile(staticFS, filePath); err == nil {
			ext := filePath[len(filePath)-len(getExt(filePath)):]
			w.Header().Set("Content-Type", contentType(ext))
			w.Write(data)
			return
		}
		if data, err := fs.ReadFile(staticFS, "index.html"); err == nil {
			w.Header().Set("Content-Type", "text/html")
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	panelURL := fmt.Sprintf("http://localhost%s", addr)
	log.Printf("🚀 MiMo Web Proxy starting on %s", panelURL)
	log.Printf("   Accounts: %d, API Key: see config.json", accountPool.Count())

	// 双击运行：启动后自动打开管理面板（设置 NO_BROWSER_OPEN=1 关闭）
	if os.Getenv("NO_BROWSER_OPEN") != "1" {
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(panelURL)
		}()
	}

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Printf("Server error: %v", err)
		// 双击运行时窗口会立刻关闭，停一停让用户看到错误
		fmt.Println("\nPress Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}
}

// resolveBaseDir 决定 config.json 与 data/ 的落盘位置。
//
// 默认跟随可执行文件所在目录（双击/直接运行时不散落文件）。
// 例外：macOS 的 .app bundle 里 exe 位于 Foo.app/Contents/MacOS/，
// 往 bundle 内写配置会破坏代码签名、装到 /Applications 时可能无写权限、
// 升级即丢配置，因此改用 ~/Library/Application Support/mimo-web-proxy。
func resolveBaseDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "darwin" && strings.Contains(exe, ".app/Contents/MacOS/") {
		if home, err := os.UserHomeDir(); err == nil {
			appDir := filepath.Join(home, "Library", "Application Support", "mimo-web-proxy")
			if err := os.MkdirAll(appDir, 0755); err == nil {
				return appDir
			}
		}
	}
	return dir
}

// openBrowser 用系统默认浏览器打开面板
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open browser: %v", err)
	}
}

func apiKeyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Authorization")
		if key == "" {
			key = r.Header.Get("x-api-key") // Anthropic 客户端标准头
		}
		if key == "" {
			key = r.Header.Get("api-key")
		}
		if len(key) > 7 && key[:7] == "Bearer " {
			key = key[7:]
		}
		currentCfg := config.Get()
		if key == "" || key != currentCfg.APIKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"Invalid API key","type":"authentication_error"}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getExt(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' {
			return ""
		}
	}
	return ""
}

func contentType(ext string) string {
	switch ext {
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
