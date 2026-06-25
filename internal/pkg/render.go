package pkg

import (
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// PageData 传递给模板的数据
type PageData struct {
	User        *UserInfo
	ActivePage  string
	ActiveGroup string // 二级导航分组: dashboard/platform/author/promote/tags/settings
	Threshold   float64
	Error       string
}

// UserInfo 用户信息（传递给模板）
type UserInfo struct {
	ID       int64
	Username string
}

var templates map[string]*template.Template

// SetupTemplates 解析所有模板（base + 每个子模板），使用 embed.FS
func SetupTemplates(_ *gin.Engine, tplFS fs.FS) {
	funcMap := template.FuncMap{
		"upper":     strings.ToUpper,
		"firstChar": firstChar,
	}

	templates = make(map[string]*template.Template)

	// 每个子模板单独与 base.html 组合解析
	pages := []string{
		"login.html",
		"dashboard.html",
		"create.html",
		"campaigns.html",
		"orders.html",
		"accounts.html",
		"authors.html",
		"settings.html",
		"strategies.html",
		"promote_logs.html",
		"videos.html",
		"video_stats.html",
		"tags.html",
	}

	for _, page := range pages {
		t := template.Must(
			template.New("").Funcs(funcMap).ParseFS(tplFS, "templates/base.html", "templates/"+page),
		)
		templates[page] = t
	}
}

// Render 渲染页面
func Render(c *gin.Context, code int, name string, data PageData) {
	t, ok := templates[name]
	if !ok {
		c.String(http.StatusInternalServerError, "template %s not found", name)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(code)
	if err := t.ExecuteTemplate(c.Writer, "base", data); err != nil {
		c.String(http.StatusInternalServerError, "render error: %v", err)
	}
}

func firstChar(s string) string {
	if s == "" {
		return ""
	}
	for _, r := range s {
		return string(r)
	}
	return ""
}
