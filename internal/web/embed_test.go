package web

import (
	"io/fs"
	"strings"
	"testing"
)

// StaticFS 是 /static/ 路由的根，必须真的含有资源；为空说明 static/ 没被打进二进制。
func TestStaticFSHasAssets(t *testing.T) {
	entries, err := fs.ReadDir(StaticFS, ".")
	if err != nil {
		t.Fatalf("StaticFS 根目录不可读: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("StaticFS 为空：static/ 未被打进二进制")
	}
}

// Templates 由 html/template 从 AssetsFS 解析，缺了等于页面渲染全挂。
func TestAssetsFSHasIndexTemplate(t *testing.T) {
	if _, err := AssetsFS.ReadFile("templates/index.html"); err != nil {
		t.Fatalf("AssetsFS 读不到 templates/index.html: %v", err)
	}
}

// AssetVersion 读取失败时回退 v1（例如模板被改名），不能 panic。
func TestAssetVersionFallsBack(t *testing.T) {
	if v := assetVersion("templates/not-exist.html"); v != "v1" {
		t.Fatalf("读取失败应回退 v1，实际 %q", v)
	}
}

// SWVersion 是 service-worker 缓存前缀，形态必须是 v+16 位十六进制的内容哈希，
// 否则 HTML 一改而版本号不变，客户端会一直用旧缓存。
func TestSWVersionIsContentHash(t *testing.T) {
	if !strings.HasPrefix(SWVersion, "v") || len(SWVersion) != 17 {
		t.Fatalf("SWVersion 不是预期的内容哈希形态: %q", SWVersion)
	}
}
