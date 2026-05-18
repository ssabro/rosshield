//go:build rosshield_enterprise

// testfixtures_test.go — hand-crafted minimal WASM 모듈 (TDD 전용 fixture).
//
// 본 파일은 evaluator 검증용 WASM 바이트를 정의합니다. TinyGo/Rust 빌드 없이
// WebAssembly 1.0 바이너리 포맷 spec 기반으로 직접 작성하여 worktree 의
// 외부 빌드 의존성 0 을 유지합니다.
//
// 각 fixture 는 다음 패턴으로 구성됩니다:
//
//	header  : 00 61 73 6d 01 00 00 00 (magic + version)
//	type    : section id=1
//	import  : section id=2 (WASI 호출 시 필요)
//	func    : section id=3
//	memory  : section id=5 (fd_write 시 필요 — 데이터 위치)
//	export  : section id=7 (_start, memory)
//	code    : section id=10
//	data    : section id=11 (fd_write 의 출력 바이트)
//
// 참조: https://webassembly.github.io/spec/core/binary/index.html

package wasmrt

// wasmMinimalEmpty 는 magic + version 만 갖는 최소 유효 WASM 입니다.
// _start 가 없어 instantiate 시 즉시 종료 (start function skipped).
//
// 본 fixture 는 다음 테스트에 사용됩니다:
//   - 정상 compile + instantiate 가능하나 stdout 출력이 0 → ErrInvalidOutput.
var wasmMinimalEmpty = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic '\0asm'
	0x01, 0x00, 0x00, 0x00, // version 1
}

// wasmInfiniteLoop 는 _start 가 무한 loop 을 도는 WASM 입니다.
//
// 본 fixture 는 CPU timeout 검증에 사용됩니다.
//
// 텍스트 표현:
//
//	(module
//	  (func $start (export "_start")
//	    (loop $L (br $L)))
//	)
//
// 바이너리 분해:
//
//	[type   sec id=1] : 1개 type — () -> ()
//	[func   sec id=3] : 1개 func — type 0
//	[export sec id=7] : 1개 export — name "_start", func index 0
//	[code   sec id=10]: func 0 body — loop ... br 0 ... end
//
//	loop 명령 = 0x03 0x40 (blocktype = empty)
//	br 0      = 0x0c 0x00
//	end       = 0x0b
//	func end  = 0x0b
//
// section 헤더 = id(1B) + payload_size(LEB128) + payload.
var wasmInfiniteLoop = mustAssembleWasm(
	// type section: 1 type = func () -> ()
	0x01, // section id
	0x04, // payload size
	0x01, // vec count = 1
	0x60, // functype tag
	0x00, // param count = 0
	0x00, // result count = 0

	// function section: 1 function with type index 0
	0x03, // section id
	0x02, // payload size
	0x01, // vec count
	0x00, // type index = 0

	// export section: 1 export "_start" -> func 0
	0x07,                         // section id
	0x0a,                         // payload size
	0x01,                         // vec count
	0x06,                         // name length = 6
	'_', 's', 't', 'a', 'r', 't', // name bytes
	0x00, // export kind = func
	0x00, // export index = 0

	// code section: 1 function body
	0x0a, // section id
	0x09, // payload size
	0x01, // vec count
	0x07, // function body size = 7
	0x00, // local decl count = 0
	0x03, // loop opcode
	0x40, // blocktype = empty
	0x0c, // br opcode
	0x00, // br target = 0 (loop)
	0x0b, // end (loop)
	0x0b, // end (func)
)

// wasmFdWriteJSON 은 fd_write 로 고정된 JSON 결과를 stdout 에 쓰는 WASM 입니다.
//
// 본 fixture 는 happy-path (pass) 와 결정론 (같은 입력 → 같은 출력) 검증에 사용됩니다.
//
// 텍스트 표현 (개념):
//
//	(module
//	  (import "wasi_snapshot_preview1" "fd_write"
//	    (func $fd_write (param i32 i32 i32 i32) (result i32)))
//	  (memory (export "memory") 1)
//	  (data (i32.const 64) "{\"status\":\"pass\"}") ; 17바이트
//	  (func $start (export "_start")
//	    ;; iovec 구성: [0] ptr=64, [4] len=17
//	    (i32.store (i32.const 0) (i32.const 64))
//	    (i32.store (i32.const 4) (i32.const 17))
//	    ;; fd_write(fd=1, iovs=0, iovs_len=1, nwritten=32)
//	    (drop (call $fd_write (i32.const 1) (i32.const 0) (i32.const 1) (i32.const 32))))
//	)
//
// stdout 결과 = {"status":"pass"}  (17바이트).
var wasmFdWriteJSON = mustAssembleWasm(
	// type section
	0x01, // section id
	0x0c, // payload size
	0x02, // vec count = 2 types
	// type 0: () -> ()  (for _start)
	0x60, 0x00, 0x00,
	// type 1: (i32, i32, i32, i32) -> (i32)  (for fd_write)
	0x60,
	0x04, 0x7f, 0x7f, 0x7f, 0x7f, // 4 params, all i32
	0x01, 0x7f, // 1 result i32

	// import section: wasi_snapshot_preview1.fd_write
	0x02,                                                                                                         // section id
	0x23,                                                                                                         // payload size = 35
	0x01,                                                                                                         // vec count = 1
	0x16,                                                                                                         // module name length = 22
	'w', 'a', 's', 'i', '_', 's', 'n', 'a', 'p', 's', 'h', 'o', 't', '_', 'p', 'r', 'e', 'v', 'i', 'e', 'w', '1', // module name
	0x08,                                   // field name length = 8
	'f', 'd', '_', 'w', 'r', 'i', 't', 'e', // field name
	0x00, // import kind = func
	0x01, // type index = 1

	// function section: 1 function with type 0  (function index = 1, since import is 0)
	0x03, // section id
	0x02, // payload size
	0x01, // vec count
	0x00, // type index = 0

	// memory section: 1 memory, min 1 page
	0x05, // section id
	0x03, // payload size
	0x01, // vec count
	0x00, // limits flag = 0 (no max)
	0x01, // min = 1 page

	// export section: 2 exports — "memory" + "_start"
	// payload = vec_count(1) + memory_export(9) + start_export(9) = 19
	0x07, // section id
	0x13, // payload size = 19
	0x02, // vec count = 2
	// export 0: "memory" -> memory 0
	0x06, 'm', 'e', 'm', 'o', 'r', 'y',
	0x02, // kind = memory
	0x00, // memory index 0
	// export 1: "_start" -> func 1 (import is 0, our func is 1)
	0x06, '_', 's', 't', 'a', 'r', 't',
	0x00, // kind = func
	0x01, // func index = 1

	// code section: 1 function body
	// 주의: i32.const 64 는 signed LEB128 에서 [0xc0, 0x00] (2바이트) — 단순 0x40 (sign bit 위반).
	// body bytes = locals(1) + insns 13개 (27바이트, 64 = 2바이트) = 28 → body_size = 0x1c
	// payload = vec(1) + body_size(1) + body(28) = 30 → 0x1e
	0x0a, // section id
	0x1e, // payload size = 30
	0x01, // vec count
	0x1c, // function body size = 28
	0x00, // local decl count = 0
	// i32.store mem[0] = 64 (iovec.ptr)
	0x41, 0x00, //   i32.const 0  (address)
	0x41, 0xc0, 0x00, //   i32.const 64 (signed LEB128, 2바이트)
	0x36, 0x02, 0x00, //   i32.store align=2 offset=0
	// i32.store mem[4] = 17 (iovec.len)
	0x41, 0x04,
	0x41, 0x11,
	0x36, 0x02, 0x00,
	// call fd_write(fd=1, iovs=0, iovs_len=1, nwritten=32)
	0x41, 0x01, //   i32.const 1 (fd)
	0x41, 0x00, //   i32.const 0 (iovs ptr)
	0x41, 0x01, //   i32.const 1 (iovs len)
	0x41, 0x20, //   i32.const 32 (nwritten ptr)
	0x10, 0x00, //   call import 0 (fd_write)
	0x1a, //   drop (discard result)
	0x0b, // end

	// data section: write "{\"status\":\"pass\"}" at memory offset 64
	// segment = prefix(1) + offset_expr(4: 0x41 0xc0 0x00 0x0b) + length(1) + data(17) = 23
	// payload = vec(1) + segment(23) = 24 → 0x18
	0x0b,                   // section id
	0x18,                   // payload size = 24
	0x01,                   // vec count = 1
	0x00,                   // active prefix (memory 0 암묵적)
	0x41, 0xc0, 0x00, 0x0b, // offset expr: i32.const 64 (signed LEB128) + end
	0x11, // data length = 17
	'{', '"', 's', 't', 'a', 't', 'u', 's', '"', ':', '"', 'p', 'a', 's', 's', '"', '}',
)

// wasmFdWriteInvalidJSON 은 stdout 에 비-JSON 바이트를 쓰는 WASM 입니다.
//
// 본 fixture 는 ErrInvalidOutput 검증에 사용됩니다. fdWriteJSON 과 같은 구조이나
// 데이터가 "not json at all" (16바이트).
var wasmFdWriteInvalidJSON = mustAssembleWasm(
	// type section (동일)
	0x01,
	0x0c,
	0x02,
	0x60, 0x00, 0x00,
	0x60,
	0x04, 0x7f, 0x7f, 0x7f, 0x7f,
	0x01, 0x7f,

	// import section (동일)
	0x02,
	0x23,
	0x01,
	0x16,
	'w', 'a', 's', 'i', '_', 's', 'n', 'a', 'p', 's', 'h', 'o', 't', '_', 'p', 'r', 'e', 'v', 'i', 'e', 'w', '1',
	0x08,
	'f', 'd', '_', 'w', 'r', 'i', 't', 'e',
	0x00,
	0x01,

	// function section (동일)
	0x03, 0x02, 0x01, 0x00,

	// memory section (동일)
	0x05, 0x03, 0x01, 0x00, 0x01,

	// export section (동일)
	0x07,
	0x13, // payload size = 19
	0x02,
	0x06, 'm', 'e', 'm', 'o', 'r', 'y',
	0x02, 0x00,
	0x06, '_', 's', 't', 'a', 'r', 't',
	0x00, 0x01,

	// code section (28바이트 body, i32.const 64 = 2바이트 LEB128)
	0x0a,
	0x1e, // payload = 30
	0x01,
	0x1c, // body = 28
	0x00,
	0x41, 0x00,
	0x41, 0xc0, 0x00, // i32.const 64 (signed LEB128)
	0x36, 0x02, 0x00,
	0x41, 0x04,
	0x41, 0x10, //   length = 16
	0x36, 0x02, 0x00,
	0x41, 0x01,
	0x41, 0x00,
	0x41, 0x01,
	0x41, 0x20,
	0x10, 0x00,
	0x1a,
	0x0b,

	// data section: "not json at all!" (16 bytes)
	// segment = prefix(1) + offset_expr(4) + length(1) + data(16) = 22
	// payload = vec(1) + segment(22) = 23 → 0x17... wait, error says 23 vs 22 → payload = 23
	// 잠깐: 새 offset_expr 가 4 바이트 (i32.const 64 = 0x41, 0xc0, 0x00 + 0x0b end) →
	// segment = 1+4+1+16 = 22, payload = 1+22 = 23 → 0x17
	0x0b,
	0x17, // payload size = 23
	0x01,
	0x00,
	0x41, 0xc0, 0x00, 0x0b,
	0x10, // data length = 16
	'n', 'o', 't', ' ', 'j', 's', 'o', 'n', ' ', 'a', 't', ' ', 'a', 'l', 'l', '!',
)

// wasmFdWriteLarge 는 stdout 에 64KB 를 훨씬 초과하는 데이터를 쓰는 WASM 입니다.
//
// 본 fixture 는 ErrStdoutTruncated 검증에 사용됩니다. memory 안 1 페이지(64KB)를
// 가득 채워서 한 번에 fd_write 합니다 — 한도 64KB 와 동등하나 한도를 초과하도록
// limits 를 32KB 로 낮춰 테스트.
var wasmFdWriteLarge = mustAssembleWasm(
	// type section
	0x01,
	0x0c,
	0x02,
	0x60, 0x00, 0x00,
	0x60,
	0x04, 0x7f, 0x7f, 0x7f, 0x7f,
	0x01, 0x7f,

	// import section
	0x02,
	0x23,
	0x01,
	0x16,
	'w', 'a', 's', 'i', '_', 's', 'n', 'a', 'p', 's', 'h', 'o', 't', '_', 'p', 'r', 'e', 'v', 'i', 'e', 'w', '1',
	0x08,
	'f', 'd', '_', 'w', 'r', 'i', 't', 'e',
	0x00,
	0x01,

	// function section
	0x03, 0x02, 0x01, 0x00,

	// memory section: 1 page (64KB)
	0x05, 0x03, 0x01, 0x00, 0x01,

	// export section
	0x07,
	0x13, // payload size = 19
	0x02,
	0x06, 'm', 'e', 'm', 'o', 'r', 'y',
	0x02, 0x00,
	0x06, '_', 's', 't', 'a', 'r', 't',
	0x00, 0x01,

	// code section: iovec.ptr=64, iovec.len=0xfb00 (64256)
	// i32.const 64 는 signed LEB128 [0xc0, 0x00] (2바이트).
	// body = locals(1) + 13 insns + extra LEB byte = 30 → body_size = 0x1e
	//        (insns: 0x41 0x00 + 0x41 0xc0 0x00 + 0x36 0x02 0x00 + 0x41 0x04 + 0x41 0x80 0xf6 0x03 +
	//                0x36 0x02 0x00 + 0x41 0x01 + 0x41 0x00 + 0x41 0x01 + 0x41 0x20 + 0x10 0x00 + 0x1a + 0x0b
	//         = 2+3+3+2+4+3+2+2+2+2+2+1+1 = 29) + locals(1) = 30
	// payload = vec(1) + body_size_LEB(1) + body(30) = 32 → 0x20
	0x0a,
	0x20, // payload = 32
	0x01,
	0x1e, // body = 30
	0x00,
	// store iovec.ptr = 64
	0x41, 0x00,
	0x41, 0xc0, 0x00, // i32.const 64 (signed LEB128)
	0x36, 0x02, 0x00,
	// store iovec.len = 0xfb00 = 64256 (LEB128: 0x80 0xf6 0x03)
	0x41, 0x04,
	0x41, 0x80, 0xf6, 0x03,
	0x36, 0x02, 0x00,
	// fd_write(1, 0, 1, 32)
	0x41, 0x01,
	0x41, 0x00,
	0x41, 0x01,
	0x41, 0x20,
	0x10, 0x00,
	0x1a,
	0x0b,

	// data section: write 64 bytes of 'A' at offset 64 (memory page 0 의 나머지는 0 — fd_write
	// 가 0byte 포함 64256바이트를 stdout 으로 흘려보낸다 — cappedWriter 가 limit 초과 분 drop).
	// segment = prefix(1) + offset_expr(4: 0x41 0xc0 0x00 0x0b) + length(1) + data(64) = 70
	// payload = vec(1) + segment(70) = 71 → 0x47
	0x0b,
	0x47, // payload = 71
	0x01,
	0x00,
	0x41, 0xc0, 0x00, 0x0b,
	0x40, // data length = 64
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
)

// mustAssembleWasm 는 magic+version + 임의 section 바이트를 합쳐 완전한 WASM 바이너리를 만듭니다.
func mustAssembleWasm(sections ...byte) []byte {
	out := make([]byte, 0, len(wasmMagic)+len(sections))
	out = append(out, wasmMagic...)
	out = append(out, sections...)
	return out
}
