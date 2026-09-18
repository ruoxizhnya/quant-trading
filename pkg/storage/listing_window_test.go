package storage

import (
	"testing"
	"time"
)

// P2-4：ListingWindow 的边界语义。
//
// 这里每一个 case 都对应一类具体的错误：把「还没上市」当成在市是未来股，
// 把「已摘牌」当成在市会让持仓挂在不存在的票上，摘牌当天判错则会让
// 退市整理期那笔最后的成交凭空消失。

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestListingWindow_IsListed(t *testing.T) {
	delist := d("2021-06-01")

	cases := []struct {
		name string
		w    ListingWindow
		at   time.Time
		want bool
	}{
		{"上市前", ListingWindow{List: d("2015-01-01")}, d("2014-12-31"), false},
		{"上市当天", ListingWindow{List: d("2015-01-01")}, d("2015-01-01"), true},
		{"在市", ListingWindow{List: d("2015-01-01")}, d("2020-06-01"), true},
		{"仍在市（无摘牌日）", ListingWindow{List: d("2015-01-01")}, d("2030-01-01"), true},

		{"摘牌前", ListingWindow{List: d("2010-01-01"), Delist: &delist}, d("2021-05-31"), true},
		{"摘牌当天", ListingWindow{List: d("2010-01-01"), Delist: &delist}, d("2021-06-01"), true},
		{"摘牌后", ListingWindow{List: d("2010-01-01"), Delist: &delist}, d("2021-06-02"), false},

		// 上市日未知：不做左端过滤。宁可放宽，也不凭空剔除 —— 剔除会
		// 悄悄缩小池子，而缩小池子本身就是幸存者偏差的一种。
		{"上市日未知", ListingWindow{Delist: nil}, d("1990-01-01"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.w.IsListed(tc.at); got != tc.want {
				t.Errorf("IsListed(%s) = %v, want %v", tc.at.Format("2006-01-02"), got, tc.want)
			}
		})
	}
}

// 摘牌日必须区分「没有」和「零值」。
//
// 用零值 time.Time 表示"未退市"是这类代码最经典的坑：0001-01-01 会让
// 每一只正常股票都被判成"一万年前就退市了"。
func TestListingWindow_NilDelistMeansStillListed(t *testing.T) {
	w := ListingWindow{List: d("2015-01-01"), Delist: nil}
	if !w.IsListed(d("2026-09-18")) {
		t.Error("Delist 为 nil 表示仍在市，不能当成零值时间")
	}
}
