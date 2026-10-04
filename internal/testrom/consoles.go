package testrom

import "encoding/binary"

// Test ROMs for the other consoles, hand-assembled like the GBA one. The
// NES, Game Boy and SNES ones keep the same counter: the first byte of
// battery-backed RAM goes up by one each time A is pressed. The DS one has
// no save; it turns the top screen from red to green while the bottom
// screen is touched, which checks taps reach the touch screen.

// For picks the test ROM for a console: "gba", "gb", "gbc", "nes",
// "snes" or "nds". nil for any other.
func For(platform string) []byte {
	switch platform {
	case "gba":
		return ROM()
	case "gb", "gbc":
		return GB()
	case "nes":
		return NES()
	case "snes":
		return SNES()
	case "nds":
		return DS()
	}
	return nil
}

// nesProgram is 6502 code at $C000:
//
//	reset: sei / cld / ldx #$ff / txs
//	       lda #0 / sta $00          ; A held last time
//	loop:  lda #1 / sta $4016 / lda #0 / sta $4016   ; latch the pad
//	       lda $4016 / and #1        ; A
//	       cmp $00 / beq loop        ; no change
//	       sta $00 / and #1 / beq loop   ; released
//	       inc $6000                 ; the save
//	       jmp loop
//	nmi:   rti
var nesProgram = []byte{
	0x78, 0xd8, 0xa2, 0xff, 0x9a, 0xa9, 0x00, 0x85, 0x00,
	0xa9, 0x01, 0x8d, 0x16, 0x40, 0xa9, 0x00, 0x8d, 0x16, 0x40,
	0xad, 0x16, 0x40, 0x29, 0x01, 0xc5, 0x00, 0xf0, 0xed,
	0x85, 0x00, 0x29, 0x01, 0xf0, 0xe7,
	0xee, 0x00, 0x60, 0x4c, 0x09, 0xc0, 0x40,
}

// NES returns an iNES ROM: mapper 0, 16 KiB of program, 8 KiB of
// graphics and battery-backed RAM at $6000.
func NES() []byte {
	prg := make([]byte, 16<<10)
	copy(prg, nesProgram)
	// NMI and IRQ go to the rti, reset to $C000.
	copy(prg[0x3ffa:], []byte{0x28, 0xc0, 0x00, 0xc0, 0x28, 0xc0})
	header := []byte{'N', 'E', 'S', 0x1a, 1, 1, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	rom := append(header, prg...)
	return append(rom, make([]byte, 8<<10)...)
}

// gbProgram is Game Boy code at $0150:
//
//	ld a,$0a / ld ($0000),a        ; RAM on
//	xor a / ldh ($80),a            ; A held last time
//	loop: ld a,$10 / ldh ($00),a   ; read the buttons
//	      ldh a,($00) / ldh a,($00) / cpl / and 1 / ld b,a
//	      ldh a,($80) / cp b / jr z,loop
//	      ld a,b / ldh ($80),a / or a / jr z,loop
//	      ld hl,$a000 / inc (hl)   ; the save
//	      jr loop
var gbProgram = []byte{
	0x3e, 0x0a, 0xea, 0x00, 0x00, 0xaf, 0xe0, 0x80,
	0x3e, 0x10, 0xe0, 0x00, 0xf0, 0x00, 0xf0, 0x00, 0x2f, 0xe6, 0x01, 0x47,
	0xf0, 0x80, 0xb8, 0x28, 0xef,
	0x78, 0xe0, 0x80, 0xb7, 0x28, 0xe9,
	0x21, 0x00, 0xa0, 0x34, 0x18, 0xe3,
}

// GB returns a 32 KiB Game Boy ROM with an MBC1, 8 KiB of RAM and a
// battery.
func GB() []byte {
	rom := make([]byte, 32<<10)
	copy(rom[0x100:], []byte{0x00, 0xc3, 0x50, 0x01}) // nop / jp $0150
	copy(rom[0x134:], "PARLOR TEST")
	rom[0x147] = 0x03 // MBC1 + RAM + battery
	rom[0x149] = 0x02 // 8 KiB
	var sum byte
	for _, b := range rom[0x134:0x14d] {
		sum = sum - b - 1
	}
	rom[0x14d] = sum
	copy(rom[0x150:], gbProgram)
	return rom
}

// snesProgram is 65816 code at $8000:
//
//	clc / xce                       ; native mode, 8-bit registers
//	lda #1 / sta $4200              ; read the pads every frame
//	lda #0 / sta $00                ; A held last time
//	loop: lda $4212 / and #1 / bne loop   ; wait for the read
//	      lda $4218 / and #$80      ; A
//	      cmp $00 / beq loop
//	      sta $00 / cmp #0 / beq loop
//	      lda $700000 / inc a / sta $700000   ; the save
//	      bra loop
var snesProgram = []byte{
	0x18, 0xfb, 0xa9, 0x01, 0x8d, 0x00, 0x42, 0xa9, 0x00, 0x85, 0x00,
	0xad, 0x12, 0x42, 0x29, 0x01, 0xd0, 0xf9,
	0xad, 0x18, 0x42, 0x29, 0x80, 0xc5, 0x00, 0xf0, 0xf0,
	0x85, 0x00, 0xc9, 0x00, 0xf0, 0xea,
	0xaf, 0x00, 0x00, 0x70, 0x1a, 0x8f, 0x00, 0x00, 0x70, 0x80, 0xdf,
}

// SNES returns a 32 KiB LoROM with 8 KiB of battery-backed RAM.
func SNES() []byte {
	rom := make([]byte, 32<<10)
	copy(rom, snesProgram)
	h := rom[0x7fc0:]
	copy(h, "PARLOR TEST          ")
	h[0x15] = 0x20 // LoROM
	h[0x16] = 0x02 // ROM + RAM + battery
	h[0x17] = 0x05 // 32 KiB
	h[0x18] = 0x03 // 8 KiB of RAM
	h[0x19] = 0x01
	binary.LittleEndian.PutUint16(rom[0x7ffc:], 0x8000) // reset
	// The checksum and its complement add up to the same whatever they are.
	binary.LittleEndian.PutUint16(h[0x1c:], 0xffff)
	var sum uint16
	for _, b := range rom {
		sum += uint16(b)
	}
	binary.LittleEndian.PutUint16(h[0x1c:], ^sum)
	binary.LittleEndian.PutUint16(h[0x1e:], sum)
	return rom
}

// dsARM9 shows red on the top screen, green while the touch screen's X
// (which dsARM7 leaves at 0x027FF000) isn't 0:
//
//	mov r0, #0x04000000 / add r2, r0, #0x304
//	mov r1, #0x8003 / strh r1, [r2]      ; POWCNT1: screens and 2D A on
//	mov r1, #0x10000 / str r1, [r0]      ; DISPCNT: display on
//	mov r3, #0x05000000                  ; the backdrop colour
//	mov r5, #0x027FF000
//	loop: mov r1, #0x1F / ldr r4, [r5] / cmp r4, #0
//	      movne r1, #0x3E0 / strh r1, [r3] / b loop
var dsARM9 = []uint32{
	0xe3a00301, 0xe2802c03, 0xe2822004, 0xe3a01902, 0xe3811003, 0xe1c210b0,
	0xe3a01801, 0xe5801000, 0xe3a03405, 0xe3a0550a, 0xe2455a01, 0xe3a0101f,
	0xe5954000, 0xe3540000, 0x13a01e3e, 0xe1c310b0, 0xeafffff9,
}

// dsARM7 reads the touch screen controller's X over SPI (command 0xD0,
// then two bytes) and stores it at 0x027FF000, forever.
var dsARM7 = []uint32{
	0xe3a00301, 0xe2800d07, 0xe3a0350a, 0xe2433a01, 0xe3a01c8a, 0xe3811001,
	0xe1c010b0, 0xe3a020d0, 0xe1c020b2, 0xe1d020b0, 0xe3120080, 0x1afffffc,
	0xe3a01c8a, 0xe3811001, 0xe1c010b0, 0xe3a02000, 0xe1c020b2, 0xe1d020b0,
	0xe3120080, 0x1afffffc, 0xe1d040b2, 0xe3a01c82, 0xe3811001, 0xe1c010b0,
	0xe3a02000, 0xe1c020b2, 0xe1d020b0, 0xe3120080, 0x1afffffc, 0xe1d050b2,
	0xe1854404, 0xe1a041a4, 0xe5834000, 0xeaffffe1,
}

// DS returns a DS ROM. Its code sits past the secure area (0x4000-0x7FFF),
// which melonDS would otherwise try to decrypt.
func DS() []byte {
	rom := make([]byte, 0x9000)
	copy(rom, "PARLOR TEST")
	copy(rom[0x0c:], "PRLR01")
	put := func(at int, v ...uint32) {
		for i, w := range v {
			binary.LittleEndian.PutUint32(rom[at+4*i:], w)
		}
	}
	put(0x20, 0x8000, 0x02000000, 0x02000000, uint32(4*len(dsARM9)))
	put(0x30, 0x8800, 0x02380000, 0x02380000, uint32(4*len(dsARM7)))
	put(0x80, 0x9000, 0x4000) // used size, header size
	put(0x8000, dsARM9...)
	put(0x8800, dsARM7...)
	binary.LittleEndian.PutUint16(rom[0x15c:], 0xcf56) // logo checksum
	binary.LittleEndian.PutUint16(rom[0x15e:], crc16(rom[:0x15e]))
	return rom
}

// crc16 is the DS header's checksum (CRC-16/MODBUS).
func crc16(data []byte) uint16 {
	c := uint16(0xffff)
	for _, b := range data {
		c ^= uint16(b)
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xa001
			} else {
				c >>= 1
			}
		}
	}
	return c
}
