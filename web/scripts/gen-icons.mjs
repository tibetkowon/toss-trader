// 설치 아이콘(PNG)을 만듭니다. 외부 라이브러리 없이 파란 바탕에 흰색 "T"를 그립니다.
// 실제 로고를 쓰려면 public/icon-192.png, icon-512.png를 교체하세요.
import { deflateSync } from 'node:zlib';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});

function crc32(buf) {
  let c = 0xffffffff;
  for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

function png(size) {
  const bg = [37, 99, 235];
  const fg = [255, 255, 255];
  const rows = [];
  const inside = (x, y) => {
    const barTop = size * 0.25, barBottom = size * 0.37;
    const stemL = size * 0.44, stemR = size * 0.56;
    const left = size * 0.25, right = size * 0.75, stemBottom = size * 0.75;
    return (y >= barTop && y <= barBottom && x >= left && x <= right) ||
      (y >= barTop && y <= stemBottom && x >= stemL && x <= stemR);
  };
  for (let y = 0; y < size; y++) {
    const row = Buffer.alloc(1 + size * 3);
    row[0] = 0; // 필터 없음
    for (let x = 0; x < size; x++) {
      const color = inside(x + 0.5, y + 0.5) ? fg : bg;
      row.set(color, 1 + x * 3);
    }
    rows.push(row);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // 비트 깊이
  ihdr[9] = 2; // 컬러 타입 RGB
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(Buffer.concat(rows))),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

for (const size of [192, 512]) {
  writeFileSync(join(here, '..', 'public', `icon-${size}.png`), png(size));
}
console.log('icons written to public/');
