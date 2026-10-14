package backtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeIndexCode 是 DATA-01「universe: csi300 走 IndexCode 展开」的 core 侧单测：
// 探索链路 YAML 默认写 `csi300`，而 index_constituents 里存的是 `000300.SH`。
// 口径不翻译 ⇒ 查询零行 ⇒ 票池空 ⇒ fail-loud invalid，看起来像「数据没灌」。
func TestNormalizeIndexCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"YAML 默认 universe", "csi300", "000300.SH"},
		{"大小写不敏感", "CSI300", "000300.SH"},
		{"带空格", "  csi300  ", "000300.SH"},
		{"中文写法", "沪深300", "000300.SH"},
		{"中证500", "csi500", "000905.SH"},
		{"中证800", "csi800", "000906.SH"},
		{"上证50", "sse50", "000016.SH"},
		{"已是库内代码则原样", "000300.SH", "000300.SH"},
		{"未登记的写法原样透传（不猜）", "someindex", "someindex"},
		{"空串", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeIndexCode(tc.in))
		})
	}
}

// TestNormalizeIndexCode_UnknownIsNotSilentlyMapped 是护栏：
// 未登记的指数若被静默映射成一个看起来合理的代码，回测就会拿**别人的成分股**
// 当票池且看不出错。未命中必须原样返回，让查询查不到、fail-loud 接管。
func TestNormalizeIndexCode_UnknownIsNotSilentlyMapped(t *testing.T) {
	got := normalizeIndexCode("csi1000")
	assert.Equal(t, "csi1000", got, "未登记指数不得被猜测映射")
}
