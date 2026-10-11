// 설치 아이콘(PNG)을 만듭니다. 외부 라이브러리 없이 그립니다.
// 파란 그라데이션 바탕에 흰색 상승 곡선과 끝점을 그립니다. 글자는 가운데 안전 영역 안에 두어서
// 일반 아이콘과 maskable 아이콘에 같은 파일을 씁니다. 가장자리는 3x3 샘플로 부드럽게 합니다.
import { deflateSync } from 'node:zlib';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const OUT = join(here, '..', 'public');

// 곡선 꼭짓점(0~1 비율 좌표)과 선 두께, 끝점 반지름입니다.
const POINTS = [
  [0.27, 0.68],
  [0.41, 0.50],
  [0.55, 0.58],
  [0.74, 0.33],
];
const STROKE = 0.068;
const DOT = 0.082;
const SAMPLES = 3;
const BG_FROM = [37, 99, 235];
const BG_TO = [30, 58, 138];

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

function distToSegment(px, py, [ax, ay], [bx, by]) {
  const dx = bx - ax;
  const dy = by - ay;
  const t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)));
  return Math.hypot(px - (ax + t * dx), py - (ay + t * dy));
}

function ink(x, y) {
  for (let i = 0; i < POINTS.length - 1; i++) {
    if (distToSegment(x, y, POINTS[i], POINTS[i + 1]) <= STROKE / 2) return true;
  }
  const end = POINTS[POINTS.length - 1];
  return Math.hypot(x - end[0], y - end[1]) <= DOT / 2;
}

function png(size) {
  const rows = [];
  for (let y = 0; y < size; y++) {
    const row = Buffer.alloc(1 + size * 3);
    row[0] = 0; // 필터 없음
    for (let x = 0; x < size; x++) {
      let hits = 0;
      for (let sy = 0; sy < SAMPLES; sy++) {
        for (let sx = 0; sx < SAMPLES; sx++) {
          const nx = (x + (sx + 0.5) / SAMPLES) / size;
          const ny = (y + (sy + 0.5) / SAMPLES) / size;
          if (ink(nx, ny)) hits++;
        }
      }
      const coverage = hits / (SAMPLES * SAMPLES);
      const t = (x + y) / (2 * size);
      for (let c = 0; c < 3; c++) {
        const background = BG_FROM[c] + (BG_TO[c] - BG_FROM[c]) * t;
        row[1 + x * 3 + c] = Math.round(background + (255 - background) * coverage);
      }
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
  writeFileSync(join(OUT, `icon-${size}.png`), png(size));
}
console.log('icons written to public/');
