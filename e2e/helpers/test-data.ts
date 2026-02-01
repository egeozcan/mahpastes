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
export function generateTestImage(
  width = 100,
  height = 100,
  color: [number, number, number] = [255, 0, 0]
): Buffer {
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
