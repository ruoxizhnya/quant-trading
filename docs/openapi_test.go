package docs

// 把 `docs/openapi.yaml` 当**规范**校验，而不是当字节流。
//
// 为什么需要：这份 spec 是 `go:embed` 直出的（`serveOpenAPISpec` 只做
// `c.Data(..., OpenAPISpec)`），全仓**没有任何地方真的解析它** —— 三条
// `TestServeOpenAPISpec_*` 只检查有没有 `openapi` / `paths` / `components` 几个
// 顶层键。所以一条语法错误、或一条指向不存在的 component 的 `$ref`，
// 可以一路合进主干：CI 全绿，Swagger UI 要到有人真的在浏览器里打开才报错。
//
// 触发这次补齐的具体情形：给 `POST /api/backtest` 加 `409` 时**新增了一条
// `$ref: '#/components/responses/ConflictPrecondition'`** —— 组件名打错一个字母，
// 上面那些测试一个都不会红。
//
// 用 `gopkg.in/yaml.v3` 而不是文本扫描：`$ref` 的解析是**结构化**的事，
// 文本扫描既会漏（缩进/引号变体）又会误报（注释里写了 `$ref`）。

import (
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// resolveLocalRef 在文档内解析 `#/a/b/c`；不是本地引用或解析不到时返回 nil。
func resolveLocalRef(doc any, ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		return nil // 外部引用，本测试不管
	}
	node := doc
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node, ok = m[part]
		if !ok {
			return nil
		}
	}
	return node
}

// collectRefs 深度遍历，收出每一条 `$ref` 及其位置。
func collectRefs(node any, path string, out *[][2]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					*out = append(*out, [2]string{path, s})
				}
				continue
			}
			collectRefs(child, path+"/"+k, out)
		}
	case []any:
		for i, child := range v {
			collectRefs(child, path+"/"+strconv.Itoa(i), out)
		}
	}
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

func TestOpenAPISpec_ParsesAsYAML(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal(OpenAPISpec, &doc); err != nil {
		t.Fatalf("docs/openapi.yaml 解析失败（它是 embed 直出的，所以这个错只会在"+
			"有人打开 Swagger UI 时暴露）：%v", err)
	}

	root, ok := doc.(map[string]any)
	if !ok {
		t.Fatalf("顶层应当是映射，实际 %T", doc)
	}
	for _, key := range []string{"openapi", "info", "paths"} {
		if _, ok := root[key]; !ok {
			t.Fatalf("缺少顶层键 %q", key)
		}
	}
}

// 每一条本地 $ref 都必须解析得到 —— 打错一个 component 名字不该能过 CI。
func TestOpenAPISpec_EveryLocalRefResolves(t *testing.T) {
	var doc any
	if err := yaml.Unmarshal(OpenAPISpec, &doc); err != nil {
		t.Fatalf("解析失败：%v", err)
	}

	var refs [][2]string
	collectRefs(doc, "", &refs)
	if len(refs) == 0 {
		t.Fatal("一条 $ref 都没收集到 —— 要么 spec 变了形，要么这个遍历是瞎的")
	}

	for _, r := range refs {
		where, ref := r[0], r[1]
		if resolveLocalRef(doc, ref) == nil {
			t.Errorf("$ref %q 解析不到（出现在 %s）", ref, where)
		}
	}
	t.Logf("本地 $ref 共 %d 条，全部可解析", len(refs))
}

// 每个 path 下的每个 HTTP 方法都必须有 responses。
func TestOpenAPISpec_EveryOperationHasResponses(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal(OpenAPISpec, &doc); err != nil {
		t.Fatalf("解析失败：%v", err)
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths 应当是映射")
	}

	ops := 0
	for path, item := range paths {
		itemMap, ok := item.(map[string]any)
		if !ok {
			t.Errorf("%s 不是映射", path)
			continue
		}
		for method, op := range itemMap {
			if !httpMethods[strings.ToLower(method)] {
				continue
			}
			ops++
			opMap, ok := op.(map[string]any)
			if !ok {
				t.Errorf("%s %s 不是映射", strings.ToUpper(method), path)
				continue
			}
			if _, ok := opMap["responses"]; !ok {
				t.Errorf("%s %s 没有 responses", strings.ToUpper(method), path)
			}
		}
	}
	if ops == 0 {
		t.Fatal("一个操作都没遍历到 —— 这个测试是瞎的")
	}
	t.Logf("端点操作 %d 个", ops)
}
