// Package testrom builds a tiny homebrew GBA ROM for tests, so nothing ever
// needs a real game. It shows a solid colour, keeps a counter in the first
// byte of SRAM and adds one each time A is pressed (wrapping at 256), so a
// test can press A and look for the new save on the server. The colour
// changes with the counter.
package testrom

import "encoding/binary"

// program is ARM code loaded at 0x080000C0:
//
//	start:    mov r0, #0x04000000      ; I/O registers
//	          mov r1, #0x400
//	          orr r1, r1, #3
//	          strh r1, [r0]            ; DISPCNT: mode 3, BG2 on
//	          mov r2, #0x0E000000      ; SRAM
//	          ldrb r5, [r2]            ; counter from the save
//	          mov r6, #0               ; A held last frame
//	          add r4, r0, #0x130       ; KEYINPUT
//	loop:     ldrh r3, [r4]
//	          tst r3, #1               ; A (0 = pressed)
//	          bne released
//	          cmp r6, #0
//	          bne draw
//	          mov r6, #1
//	          add r5, r5, #1
//	          and r5, r5, #0xff
//	          strb r5, [r2]            ; write the save
//	          b draw
//	released: mov r6, #0
//	draw:     and r7, r5, #31          ; colour from the counter
//	          orr r7, r7, #0x3e0
//	          tst r5, #1
//	          orreq r7, r7, #0x7c00
//	          mov r8, #0x06000000      ; VRAM
//	          mov r9, #0x9600          ; 240×160 pixels
//	fill:     strh r7, [r8], #2
//	          subs r9, r9, #1
//	          bne fill
//	          b loop
var program = []uint32{
	0xe3a00301, 0xe3a01b01, 0xe3811003, 0xe1c010b0, 0xe3a0240e, 0xe5d25000,
	0xe3a06000, 0xe2804e13, 0xe1d430b0, 0xe3130001, 0x1a000006, 0xe3560000,
	0x1a000005, 0xe3a06001, 0xe2855001, 0xe20550ff, 0xe5c25000, 0xea000000,
	0xe3a06000, 0xe205701f, 0xe3877e3e, 0xe3150001, 0x03877b1f, 0xe3a08406,
	0xe3a09c96, 0xe0c870b2, 0xe2599001, 0x1afffffc, 0xeaffffea,
}

// ROM returns the ROM image. Its title is "PARLOR TEST" and it declares
// SRAM, so mGBA saves 32 KiB.
func ROM() []byte {
	rom := make([]byte, 0x400)
	binary.LittleEndian.PutUint32(rom, 0xea00002e) // b 0x080000C0
	copy(rom[0xA0:], "PARLOR TEST")
	copy(rom[0xAC:], "PRLT01")
	rom[0xB2] = 0x96
	var sum byte
	for _, b := range rom[0xA0:0xBD] {
		sum -= b
	}
	rom[0xBD] = sum - 0x19
	for i, w := range program {
		binary.LittleEndian.PutUint32(rom[0xC0+4*i:], w)
	}
	copy(rom[0x300:], "SRAM_V113")
	return rom
}
