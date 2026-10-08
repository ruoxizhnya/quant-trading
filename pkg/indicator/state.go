// indicator 算子共用状态工具（K3 切片 1）。
//
// 本文件承载三算子（RMA / EWMA / Kalman）状态序列化的**共用解码器**与
// 有限性校验，保证所有算子的 SaveState / LoadState 语义一致：
//   - SaveState：版本化 JSON（version + 全部内部量）；
//   - LoadState：fail-loud（空 / null / 版本不符 / 缺字段 / 未知字段 /
//     类型不符 → error），**原子提交**（失败不改动接收者）。
package indicator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// isFinite 报告 x 是否为有限值（非 NaN、非 ±Inf）。算子的所有标量输入
// 都要过这道——ADR-028 §9：停牌 = unknown，NaN 不得静默进入递推核。
func isFinite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// decodeVersionedState 是各算子 LoadState 的共用解码器，只做解码与校验，
// **不接触接收者**——调用方在返回 nil 后才原子提交。
//
// 检查顺序与语义：
//  1. 空字节 → error（区分「没状态」与「空状态」）；
//  2. 严格解码到 dst：拒绝未知字段、拒绝类型不符；
//  3. raw map 检查 null（json "null" 解到 struct 是零值且无错，必须单判）；
//  4. required 字段逐个存在性检查（用 raw map 而非零值判缺失——像 count=0
//     是合法状态，零值不等于缺失）；
//  5. version 字段必须存在且 == wantVersion。
func decodeVersionedState(b []byte, dst any, wantVersion int, required ...string) error {
	if len(b) == 0 {
		return errors.New("indicator: LoadState 收到空状态")
	}
	// 严格解码到 dst。
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("indicator: LoadState 解析失败: %w", err)
	}
	// raw map 做 null / 缺字段 / version 检查。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("indicator: LoadState 解析失败: %w", err)
	}
	if raw == nil {
		return errors.New("indicator: LoadState 收到 null 状态")
	}
	for _, k := range required {
		if _, ok := raw[k]; !ok {
			return fmt.Errorf("indicator: LoadState 缺字段 %q", k)
		}
	}
	var v int
	if err := json.Unmarshal(raw["version"], &v); err != nil {
		return fmt.Errorf("indicator: LoadState version 解析失败: %w", err)
	}
	if v != wantVersion {
		return fmt.Errorf("indicator: LoadState 版本不匹配: got %d want %d", v, wantVersion)
	}
	return nil
}
