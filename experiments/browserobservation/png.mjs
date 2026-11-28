import zlib from "node:zlib";

function paeth(a, b, c) {
  const p = a + b - c;
  const pa = Math.abs(p - a);
  const pb = Math.abs(p - b);
  const pc = Math.abs(p - c);
  if (pa <= pb && pa <= pc) return a;
  if (pb <= pc) return b;
  return c;
}

/** Decode an 8-bit non-interlaced RGB or RGBA PNG. Returns null if unsupported. */
export function decodePNG(buffer) {
  const buf = Buffer.isBuffer(buffer) ? buffer : Buffer.from(buffer);
  if (buf.length < 8 || buf.readUInt32BE(0) !== 0x89504e47) return null;
  let offset = 8;
  let width = 0;
  let height = 0;
  let bitDepth = 0;
  let colorType = 0;
  let interlace = 0;
  const idat = [];
  while (offset + 8 <= buf.length) {
    const length = buf.readUInt32BE(offset);
    offset += 4;
    const type = buf.toString("ascii", offset, offset + 4);
    offset += 4;
    if (offset + length + 4 > buf.length) return null;
    const data = buf.subarray(offset, offset + length);
    offset += length + 4;
    if (type === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      bitDepth = data[8];
      colorType = data[9];
      interlace = data[12];
    } else if (type === "IDAT") {
      idat.push(data);
    } else if (type === "IEND") {
      break;
    }
  }
  if (bitDepth !== 8 || interlace !== 0 || (colorType !== 2 && colorType !== 6)) return null;
  const bpp = colorType === 6 ? 4 : 3;
  const raw = zlib.inflateSync(Buffer.concat(idat));
  const stride = width * bpp;
  const out = Buffer.alloc(height * stride);
  let source = 0;
  let dest = 0;
  for (let y = 0; y < height; y += 1) {
    const filter = raw[source];
    source += 1;
    for (let i = 0; i < stride; i += 1) {
      const value = raw[source];
      source += 1;
      const left = i >= bpp ? out[dest + i - bpp] : 0;
      const up = y > 0 ? out[dest + i - stride] : 0;
      const upLeft = y > 0 && i >= bpp ? out[dest + i - stride - bpp] : 0;
      let decoded = value;
      if (filter === 1) decoded = (value + left) & 255;
      else if (filter === 2) decoded = (value + up) & 255;
      else if (filter === 3) decoded = (value + Math.floor((left + up) / 2)) & 255;
      else if (filter === 4) decoded = (value + paeth(left, up, upLeft)) & 255;
      else if (filter !== 0) return null;
      out[dest + i] = decoded;
    }
    dest += stride;
  }
  return { width, height, bpp, data: out };
}

export function sampleRegion(png, box, predicate) {
  if (!png || !box) return { hits: 0, seen: 0 };
  const x0 = Math.max(0, Math.floor(box.x));
  const y0 = Math.max(0, Math.floor(box.y));
  const x1 = Math.min(png.width, Math.ceil(box.x + box.width));
  const y1 = Math.min(png.height, Math.ceil(box.y + box.height));
  let hits = 0;
  let seen = 0;
  for (let y = y0; y < y1; y += 2) {
    for (let x = x0; x < x1; x += 2) {
      seen += 1;
      const index = (y * png.width + x) * png.bpp;
      if (predicate(png.data[index], png.data[index + 1], png.data[index + 2])) hits += 1;
    }
  }
  return { hits, seen };
}

export function isMagenta(r, g, b) {
  return r > 160 && b > 160 && g < 90;
}

export function isYellow(r, g, b) {
  return r > 220 && g > 170 && b < 80;
}

export function isGreen(r, g, b) {
  return r < 40 && g > 140 && b < 40;
}
