// RetroArch's save states are bare; Parlor keeps them inside a PNG of the
// screen, as mGBA does with its own, so slots show what they hold. The
// state goes in a private chunk ("prLs") before the image's end.
const signature = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const CHUNK = "prLs";

let table: Uint32Array | null = null;
function crc32(data: Uint8Array): number {
  if (!table) {
    table = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      table[n] = c >>> 0;
    }
  }
  let c = 0xffffffff;
  for (const b of data) c = table[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

export function isPNG(data: Uint8Array): boolean {
  return data.length > 8 && signature.every((b, i) => data[i] === b);
}

// wrap puts a state into a PNG, before its IEND chunk.
export function wrap(png: Uint8Array, state: Uint8Array): Uint8Array {
  const end = png.length - 12; // IEND is the last 12 bytes
  const chunk = new Uint8Array(12 + state.length);
  const view = new DataView(chunk.buffer);
  view.setUint32(0, state.length);
  for (let i = 0; i < 4; i++) chunk[4 + i] = CHUNK.charCodeAt(i);
  chunk.set(state, 8);
  view.setUint32(8 + state.length, crc32(chunk.subarray(4, 8 + state.length)));
  const out = new Uint8Array(png.length + chunk.length);
  out.set(png.subarray(0, end));
  out.set(chunk, end);
  out.set(png.subarray(end), end + chunk.length);
  return out;
}

// unwrap takes the state out of a PNG made by wrap; anything else comes
// back as it is (a bare state, from RomM).
export function unwrap(data: Uint8Array): Uint8Array | null {
  if (!isPNG(data)) return data;
  const view = new DataView(data.buffer, data.byteOffset, data.byteLength);
  for (let at = 8; at + 12 <= data.length;) {
    const len = view.getUint32(at);
    const type = String.fromCharCode(...data.subarray(at + 4, at + 8));
    if (type === CHUNK) return data.subarray(at + 8, at + 8 + len);
    at += 12 + len;
  }
  return null;
}
