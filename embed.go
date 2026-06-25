package promote

import "embed"

// TemplatesFS 嵌入 templates/ 目录的全部 HTML 模板文件。
//
//go:embed templates/*
var TemplatesFS embed.FS

// StaticFS 嵌入 static/ 目录的全部静态资源。
//
//go:embed static/css/* static/js/*
var StaticFS embed.FS
