package main

import (
	"crypto/sha256"
	"encoding/hex"
	"embed"
	"io/fs"
)

// assetsFS 在编译期将 static/ 与 templates/ 打进二进制，使发布产物为自包含单文件，
// 不再依赖运行时文件系统上的 static/ 与 templates/ 目录（无需与二进制一同分发）。
//
//go:embed static templates
var assetsFS embed.FS

// staticFS 是 assetsFS 中以 static/ 为根的子文件系统，供 /static/ 路由直接 ServeFS。
var staticFS = mustSub(assetsFS, "static")

// swVersion 基于内嵌 templates/index.html 的内容哈希生成。HTML 内容一变，版本号即变，
// 使 service-worker 的静态缓存前缀自动失效、客户端重新拉取，等价于原先的「修改时间版本号」。
var swVersion = assetVersion("templates/index.html")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func assetVersion(name string) string {
	b, err := assetsFS.ReadFile(name)
	if err != nil {
		return "v1"
	}
	sum := sha256.Sum256(b)
	return "v" + hex.EncodeToString(sum[:8])
}
