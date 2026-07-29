import { execFile } from 'child_process';
import { access, mkdir, rename, rm } from 'fs/promises';
import { dirname, join, resolve } from 'path';
import { fileURLToPath } from 'url';
import { promisify } from 'util';

const execFileAsync = promisify(execFile);

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const repoRoot = resolve(__dirname, '../..');

const BIN_NAME = process.platform === 'win32' ? 'mahpastesd.exe' : 'mahpastesd';
const BIN_PATH = join(repoRoot, 'build', 'bin', BIN_NAME);

export function serverBinaryPath(): string {
  return process.env.MAHPASTESD_BIN ?? BIN_PATH;
}

async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

// `go build` is incremental, so rebuilding every run costs ~nothing on a warm
// cache and guarantees the server tests exercise the current Go sources.
// Parallel workers are safe: go's build cache handles concurrent invocations,
// each writes its own staging file, and the rename into place is atomic.
async function build(): Promise<string> {
  await mkdir(dirname(BIN_PATH), { recursive: true });
  const staging = `${BIN_PATH}.${process.pid}.building`;

  try {
    await execFileAsync('go', ['build', '-o', staging, './cmd/mahpastesd'], {
      cwd: repoRoot,
      maxBuffer: 10 * 1024 * 1024,
    });
    await rename(staging, BIN_PATH);
  } catch (err: any) {
    await rm(staging, { force: true }).catch(() => {});
    throw new Error(`Failed to build mahpastesd: ${err.stderr || err.message}`);
  }

  return BIN_PATH;
}

// One build per process, however many tests ask for the binary.
let pending: Promise<string> | null = null;

// Guarantees the headless server binary exists, building it when needed.
// A caller-supplied MAHPASTESD_BIN is used as-is and never rebuilt.
export async function ensureServerBinary(): Promise<string> {
  const override = process.env.MAHPASTESD_BIN;
  if (override) {
    if (!(await exists(override))) {
      throw new Error(`MAHPASTESD_BIN points at ${override}, which does not exist.`);
    }
    return override;
  }

  pending ??= build();
  return pending;
}
