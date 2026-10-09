package wasm

// 极简 WASM binary 编码器（测试用）。参考 wazero 官方测试的 encodeModule
// 做法，只支持本测试需要的子集：type / function / memory / export / code
// 五个 section，无 import、无 data、无 global。
//
// 本文件只服务 sandbox_test.go，不引入生产代码路径。

// 值类型
const (
	valI32 byte = 0x7f
	valI64 byte = 0x7e
)

// 指令 opcode（本测试用到的子集）
const (
	opLocalGet      byte = 0x20
	opI64ExtendI32U byte = 0xad
	opI64Const      byte = 0x42
	opI64Shl        byte = 0x86
	opI64Or         byte = 0x84
	opI32Const      byte = 0x41
	opBr            byte = 0x0c
	opLoop          byte = 0x03
	opEnd           byte = 0x0b
)

// wasmFuncType 描述一个函数签名。
type wasmFuncType struct {
	params  []byte
	results []byte
}

// wasmExport 描述一个导出（name + kind + index）。
// kind: 0=func, 2=memory。
type wasmExport struct {
	name string
	kind byte
	idx  uint32
}

// wasmMemory 描述一个内存声明（min 页）。
type wasmMemory struct {
	min uint32
}

// wasmModule 是本编码器支持的最小模块模型。
type wasmModule struct {
	types  []wasmFuncType
	funcs  []uint32 // 函数 → 类型索引
	memory *wasmMemory
	export []wasmExport
	codes  [][]byte // 函数体（不含 locals 前缀，本测试无局部变量）
}

// encode 序列化为合法 wasm 二进制。
func (m *wasmModule) encode() []byte {
	var buf []byte
	buf = append(buf, 0x00, 0x61, 0x73, 0x6d) // \0asm
	buf = append(buf, 0x01, 0x00, 0x00, 0x00) // version 1

	// Type section (id=1)
	buf = appendSection(buf, 1, func(s []byte) []byte {
		s = appendUleb128(s, uint32(len(m.types)))
		for _, ft := range m.types {
			s = append(s, 0x60) // func type
			s = appendUleb128(s, uint32(len(ft.params)))
			s = append(s, ft.params...)
			s = appendUleb128(s, uint32(len(ft.results)))
			s = append(s, ft.results...)
		}
		return s
	})

	// Function section (id=3)
	buf = appendSection(buf, 3, func(s []byte) []byte {
		s = appendUleb128(s, uint32(len(m.funcs)))
		for _, idx := range m.funcs {
			s = appendUleb128(s, idx)
		}
		return s
	})

	// Memory section (id=5)
	if m.memory != nil {
		buf = appendSection(buf, 5, func(s []byte) []byte {
			s = appendUleb128(s, 1)            // 1 memory
			s = append(s, 0x00)                // flags (no max)
			s = appendUleb128(s, m.memory.min) // min pages
			return s
		})
	}

	// Export section (id=7)
	if len(m.export) > 0 {
		buf = appendSection(buf, 7, func(s []byte) []byte {
			s = appendUleb128(s, uint32(len(m.export)))
			for _, exp := range m.export {
				s = appendUleb128(s, uint32(len(exp.name)))
				s = append(s, exp.name...)
				s = append(s, exp.kind)
				s = appendUleb128(s, exp.idx)
			}
			return s
		})
	}

	// Code section (id=10)
	buf = appendSection(buf, 10, func(s []byte) []byte {
		s = appendUleb128(s, uint32(len(m.codes)))
		for _, body := range m.codes {
			funcBody := appendUleb128(nil, 0) // 0 locals
			funcBody = append(funcBody, body...)
			s = appendUleb128(s, uint32(len(funcBody)))
			s = append(s, funcBody...)
		}
		return s
	})

	return buf
}

// appendSection 包装一个 section：id + 内容长度 + 内容。
func appendSection(buf []byte, id byte, buildContent func([]byte) []byte) []byte {
	content := buildContent(nil)
	buf = append(buf, id)
	buf = appendUleb128(buf, uint32(len(content)))
	buf = append(buf, content...)
	return buf
}

// appendUleb128 追加无符号 LEB128 编码。
func appendUleb128(b []byte, v uint32) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}
