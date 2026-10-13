// K0-P2-3b：声明即登记的 AST 护栏（**内部测试包**）。
//
// topic 注册表收敛后，「新增 topic 未登记」已结构上不可能（声明 TopicX =
// topic("x.y") 本身就是登记动作）。唯一剩下的绕过路径是**违规写法**：
// 有人绕开 topic() 直接写裸字符串（`TopicX = "x.y"`）或把 topic 声明回
// const ——这种写法既不校验命名规范、也不登记 registry，静默失效的老毛病
// 会从后门溜回来。本护栏在 AST 层面把这条后门焊死：
//
//	破坏验证（K0-P2-3b）：把 topics.go 里任一 TopicX = topic("...") 改成
//	TopicX = "..."（裸字符串），本文件 TestTopicDeclarationsGoThroughTopic
//	变红。
package msgbus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestTopicDeclarationsGoThroughTopic 扫描包内全部非测试源文件，断言
// 每个导出的 Topic* 声明都满足两条：
//
//  1. 只能是 var 声明（const 声明 = 绕过登记，直接红）；
//  2. 初始化器必须是 topic(<字符串字面量>) 调用（裸字符串赋值、拼接、
//     变量引用都算绕过）。
//
// 同时顺带断言 topic 调用的字面量参数本身符合命名正则——即便有人写了
// topic() 但拼错了名字，这里也会给出可读的错误而不是构造期 panic。
func TestTopicDeclarationsGoThroughTopic(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("解析 pkg/msgbus 源文件失败: %v", err)
	}

	seen := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range gen.Specs {
					valSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range valSpec.Names {
						if !strings.HasPrefix(name.Name, "Topic") {
							continue
						}
						seen++
						if gen.Tok == token.CONST {
							t.Errorf("%s: %s 用 const 声明——const 不经过 topic() 登记，是绕过注册表的违规写法（K0-P2-3b）",
								fset.Position(name.Pos()), name.Name)
							continue
						}
						if i >= len(valSpec.Values) {
							t.Errorf("%s: %s 没有初始化器（必须写成 %s = topic(\"...\")）",
								fset.Position(name.Pos()), name.Name, name.Name)
							continue
						}
						call, ok := valSpec.Values[i].(*ast.CallExpr)
						if !ok {
							t.Errorf("%s: %s 的初始化器不是 topic(...) 调用（裸字符串赋值绕过了登记）",
								fset.Position(name.Pos()), name.Name)
							continue
						}
						fn, ok := call.Fun.(*ast.Ident)
						if !ok || fn.Name != "topic" {
							t.Errorf("%s: %s 的初始化器不是 topic(...) 调用，而是 %v",
								fset.Position(name.Pos()), name.Name, call.Fun)
							continue
						}
						if len(call.Args) != 1 {
							t.Errorf("%s: %s 的 topic() 参数个数 = %d, want 1",
								fset.Position(name.Pos()), name.Name, len(call.Args))
							continue
						}
						lit, ok := call.Args[0].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							t.Errorf("%s: %s 的 topic() 参数不是字符串字面量（登记名必须是编译期常量）",
								fset.Position(name.Pos()), name.Name)
							continue
						}
						value, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Errorf("%s: %s 的 topic() 字面量无法解析: %v",
								fset.Position(name.Pos()), name.Name, err)
							continue
						}
						if !topicNamePattern.MatchString(value) {
							t.Errorf("%s: %s 的登记名 %q 不符合命名正则 ^[a-z]+(\\.[a-z_]+)+$",
								fset.Position(name.Pos()), name.Name, value)
						}
					}
				}
			}
		}
	}

	// 护栏自身失效（比如 AST 结构变了导致全部循环空转）时不能静默恒绿。
	const wantSeen = 16
	if seen != wantSeen {
		t.Errorf("扫描到 %d 个 Topic* 声明, want %d（护栏空转或声明数变了——前者是护栏坏了，后者要走变更评审）",
			seen, wantSeen)
	}
}
