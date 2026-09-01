import * as fs from 'fs/promises';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';
import * as zlib from 'zlib';

// Generate a unique ID for test data
export function uniqueId(): string {
  return crypto.randomBytes(4).toString('hex');
}

// Generate a unique filename with given extension
export function uniqueFilename(ext: string): string {
  return `test-${uniqueId()}.${ext}`;
}

// Generate test text content with optional prefix
export function generateTestText(prefix = 'test'): string {
  return `${prefix}-${uniqueId()}\nGenerated at ${new Date().toISOString()}\nLine 3 of test content`;
}

// Generate a simple PNG image as a Buffer
// Creates a solid color image with optional text marker
/**
 * Colour names accepted by generateTestImage. Callers across the suite pass
 * names ('red', 'blue') rather than tuples; before these were mapped, a string
 * fell through Buffer.from() as 0,0,0 and every "differently coloured" image
 * came out identical black.
 */
const NAMED_COLORS: Record<string, [number, number, number]> = {
  red: [255, 0, 0],
  green: [0, 255, 0],
  blue: [0, 0, 255],
  yellow: [255, 255, 0],
  purple: [128, 0, 128],
  orange: [255, 165, 0],
  cyan: [0, 255, 255],
  black: [0, 0, 0],
  white: [255, 255, 255],
};

export function generateTestImage(
  width = 100,
  height = 100,
  colorInput: [number, number, number] | keyof typeof NAMED_COLORS = [255, 0, 0]
): Buffer {
  const color: [number, number, number] = typeof colorInput === 'string'
    ? (NAMED_COLORS[colorInput] ?? [255, 0, 0])
    : colorInput;
  // Minimal valid PNG with specified dimensions and color
  // This creates a simple solid-color PNG without external dependencies

  // PNG signature
  const signature = Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]);

  // IHDR chunk (image header)
  const ihdrData = Buffer.alloc(13);
  ihdrData.writeUInt32BE(width, 0); // width
  ihdrData.writeUInt32BE(height, 4); // height
  ihdrData.writeUInt8(8, 8); // bit depth
  ihdrData.writeUInt8(2, 9); // color type (RGB)
  ihdrData.writeUInt8(0, 10); // compression
  ihdrData.writeUInt8(0, 11); // filter
  ihdrData.writeUInt8(0, 12); // interlace

  const ihdrChunk = createPNGChunk('IHDR', ihdrData);

  // IDAT chunk (image data)
  // Create raw pixel data (filter byte + RGB for each pixel per row)
  const rawData: number[] = [];
  for (let y = 0; y < height; y++) {
    rawData.push(0); // filter byte (none)
    for (let x = 0; x < width; x++) {
      rawData.push(color[0], color[1], color[2]);
    }
  }

  // Compress with zlib (deflate)
  const compressed = zlib.deflateSync(Buffer.from(rawData));
  const idatChunk = createPNGChunk('IDAT', compressed);

  // IEND chunk (image end)
  const iendChunk = createPNGChunk('IEND', Buffer.alloc(0));

  return Buffer.concat([signature, ihdrChunk, idatChunk, iendChunk]);
}

// Small generated H.264/MP4 fixture: 160×90, 0.6 seconds, with a visible
// stone-coloured frame. Keeping the bytes inline makes video tests independent
// of ffmpeg or other host media tools.
export function generateTestVideo(): Buffer {
  return Buffer.from(
    'AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAAOCbW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAAD6AAAAlgAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAq10cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAAAAlgAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAKAAAABaAAAAAAAkZWR0cwAAABxlbHN0AAAAAAAAAAEAAAJYAAAIAAABAAAAAAIlbWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAAAoAAAAGABVxAAAAAAALWhkbHIAAAAAAAAAAHZpZGUAAAAAAAAAAAAAAABWaWRlb0hhbmRsZXIAAAAB0G1pbmYAAAAUdm1oZAAAAAEAAAAAAAAAAAAAACRkaW5mAAAAHGRyZWYAAAAAAAAAAQAAAAx1cmwgAAAAAQAAAZBzdGJsAAAAwHN0c2QAAAAAAAAAAQAAALBhdmMxAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAKAAWgBIAAAASAAAAAAAAAABFUxhdmM2Mi4xMS4xMDAgbGlieDI2NAAAAAAAAAAAAAAAGP//AAAANmF2Y0MBZAAK/+EAGWdkAAqs2UKN+TARAAADAAEAAAMAFA8SJZYBAAZo6+PLIsD9+PgAAAAAEHBhc3AAAAABAAAAAQAAABRidHJ0AAAAAAAALf0AAAAAAAAAGHN0dHMAAAAAAAAAAQAAAAYAAAQAAAAAFHN0c3MAAAAAAAAAAQAAAAEAAABAY3R0cwAAAAAAAAAGAAAAAQAACAAAAAABAAAUAAAAAAEAAAgAAAAAAQAAAAAAAAABAAAEAAAAAAEAAAgAAAAAHHN0c2MAAAAAAAAAAQAAAAEAAAAGAAAAAQAAACxzdHN6AAAAAAAAAAAAAAAGAAADJwAAABAAAAANAAAADQAAAA0AAAAVAAAAFHN0Y28AAAAAAAAAAQAAA7IAAABhdWR0YQAAAFltZXRhAAAAAAAAACFoZGxyAAAAAAAAAABtZGlyYXBwbAAAAAAAAAAAAAAAACxpbHN0AAAAJKl0b28AAAAcZGF0YQAAAAEAAAAATGF2ZjYyLjMuMTAwAAAACGZyZWUAAAN7bWRhdAAAAq4GBf//qtxF6b3m2Ui3lizYINkj7u94MjY0IC0gY29yZSAxNjUgcjMyMjIgYjM1NjA1YSAtIEguMjY0L01QRUctNCBBVkMgY29kZWMgLSBDb3B5bGVmdCAyMDAzLTIwMjUgLSBodHRwOi8vd3d3LnZpZGVvbGFuLm9yZy94MjY0Lmh0bWwgLSBvcHRpb25zOiBjYWJhYz0xIHJlZj0zIGRlYmxvY2s9MTowOjAgYW5hbHlzZT0weDM6MHgxMTMgbWU9aGV4IHN1Ym1lPTcgcHN5PTEgcHN5X3JkPTEuMDA6MC4wMCBtaXhlZF9yZWY9MSBtZV9yYW5nZT0xNiBjaHJvbWFfbWU9MSB0cmVsbGlzPTEgOHg4ZGN0PTEgY3FtPTAgZGVhZHpvbmU9MjEsMTEgZmFzdF9wc2tpcD0xIGNocm9tYV9xcF9vZmZzZXQ9LTIgdGhyZWFkcz0zIGxvb2thaGVhZF90aHJlYWRzPTEgc2xpY2VkX3RocmVhZHM9MCBucj0wIGRlY2ltYXRlPTEgaW50ZXJsYWNlZD0wIGJsdXJheV9jb21wYXQ9MCBjb25zdHJhaW5lZF9pbnRyYT0wIGJmcmFtZXM9MyBiX3B5cmFtaWQ9MiBiX2FkYXB0PTEgYl9iaWFzPTAgZGlyZWN0PTEgd2VpZ2h0Yj0xIG9wZW5fZ29wPTAgd2VpZ2h0cD0yIGtleWludD0yNTAga2V5aW50X21pbj0xMCBzY2VuZWN1dD00MCBpbnRyYV9yZWZyZXNoPTAgcmNfbG9va2FoZWFkPTQwIHJjPWNyZiBtYnRyZWU9MSBjcmY9MjMuMCBxY29tcD0wLjYwIHFwbWluPTAgcXBtYXg9NjkgcXBzdGVwPTQgaXBfcmF0aW89MS40MCBhcT0xOjEuMDAAgAAAAHFliIQAEP/+5sD5ljcVdkDs9/4eh6h1AF+wN9Q0DYeVXNP8+Bk/UmiTFtuHF/RV2of+JYIKTOIpyC7xs1rGpSCYgX01gXTzO2Q+XgGkBT5EEgBKuueFYBE7m8HbKrJzUTmR6Uoq03J2SNW6olll+4VpkQAAAAxBmiRsQ7/+qZYA5oAAAAAJQZ5CeId/AGhBAAAACQGeYXRDfwCUgAAAAAkBnmNqQ38AlIEAAAARQZplSahBaJlMCG///qeEAcc=',
    'base64',
  );
}

function createPNGChunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(data.length, 0);

  const typeBuffer = Buffer.from(type, 'ascii');
  const crc = crc32(Buffer.concat([typeBuffer, data]));
  const crcBuffer = Buffer.alloc(4);
  crcBuffer.writeUInt32BE(crc, 0);

  return Buffer.concat([length, typeBuffer, data, crcBuffer]);
}

// CRC32 implementation for PNG chunks
function crc32(data: Buffer): number {
  let crc = 0xffffffff;
  const table = getCRC32Table();
  for (let i = 0; i < data.length; i++) {
    crc = table[(crc ^ data[i]) & 0xff] ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

let crc32Table: Uint32Array | null = null;
function getCRC32Table(): Uint32Array {
  if (crc32Table) return crc32Table;
  crc32Table = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let j = 0; j < 8; j++) {
      c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    crc32Table[i] = c;
  }
  return crc32Table;
}

// Create a temporary file with given content and return its path
export async function createTempFile(
  content: Buffer | string,
  ext: string
): Promise<string> {
  const filename = uniqueFilename(ext);
  const filepath = path.join(os.tmpdir(), filename);
  await fs.writeFile(filepath, content);
  return filepath;
}

// Create a temporary directory and return its path
export async function createTempDir(): Promise<string> {
  const dirname = `mahpastes-test-${uniqueId()}`;
  const dirpath = path.join(os.tmpdir(), dirname);
  await fs.mkdir(dirpath, { recursive: true });
  return dirpath;
}

// Clean up a temporary file or directory
export async function cleanup(filepath: string): Promise<void> {
  try {
    const stat = await fs.stat(filepath);
    if (stat.isDirectory()) {
      await fs.rm(filepath, { recursive: true, force: true });
    } else {
      await fs.unlink(filepath);
    }
  } catch {
    // Ignore if doesn't exist
  }
}

// Generate JSON test content
export function generateTestJSON(): string {
  return JSON.stringify(
    {
      id: uniqueId(),
      timestamp: new Date().toISOString(),
      data: {
        items: [1, 2, 3],
        nested: { value: 'test' },
      },
    },
    null,
    2
  );
}

// Generate HTML test content
export function generateTestHTML(): string {
  return `<!DOCTYPE html>
<html>
<head><title>Test ${uniqueId()}</title></head>
<body>
  <h1>Test Content</h1>
  <p>Generated at ${new Date().toISOString()}</p>
</body>
</html>`;
}

// Point type for canvas operations
export interface Point {
  x: number;
  y: number;
}

// Generate random points for drawing tests
export function generateDrawingPath(
  start: Point,
  end: Point,
  steps = 5
): Point[] {
  const points: Point[] = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    points.push({
      x: Math.round(start.x + (end.x - start.x) * t),
      y: Math.round(start.y + (end.y - start.y) * t),
    });
  }
  return points;
}
