import { ChildProcess, execFile, spawn } from 'child_process';
import type { APIRequest } from '@playwright/test';
import { access, mkdtemp, readFile, rm } from 'fs/promises';
import { tmpdir } from 'os';
import { dirname, join, resolve } from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const repoRoot = resolve(__dirname, '../..');

export type ServerHandle = {
  proc: ChildProcess;
  dataDir: string;
  port: number;
  url: string;
  bootstrapKey: string;
  logs: () => string;
  stop: () => Promise<void>;
};

type Waiter = {
  regex: RegExp;
  resolve: (match: RegExpMatchArray) => void;
  reject: (err: Error) => void;
  timer: NodeJS.Timeout;
};

export async function spawnServer(opts: { port?: number } = {}): Promise<ServerHandle> {
  const dataDir = await mkdtemp(join(tmpdir(), 'mahpastesd-e2e-'));
  const bin = process.env.MAHPASTESD_BIN ?? join(repoRoot, 'build', 'bin', process.platform === 'win32' ? 'mahpastesd.exe' : 'mahpastesd');

  try {
    await access(bin);
  } catch {
    await rm(dataDir, { recursive: true, force: true });
    throw new Error(`mahpastesd binary not found at ${bin}. Run "make mahpastesd" first or set MAHPASTESD_BIN.`);
  }

  let logs = '';
  const waiters: Waiter[] = [];
  const proc = spawn(bin, [], {
    cwd: repoRoot,
    env: {
      ...process.env,
      MAHPASTES_DATA_DIR: dataDir,
      MAHPASTESD_PORT: String(opts.port ?? 0),
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });

  const appendLog = (chunk: Buffer) => {
    logs += chunk.toString();
    for (let i = waiters.length - 1; i >= 0; i--) {
      const waiter = waiters[i];
      const match = logs.match(waiter.regex);
      if (match) {
        clearTimeout(waiter.timer);
        waiters.splice(i, 1);
        waiter.resolve(match);
      }
    }
  };
  proc.stdout?.on('data', appendLog);
  proc.stderr?.on('data', appendLog);
  proc.once('exit', (code, signal) => {
    const err = new Error(`mahpastesd exited before it was ready (code=${code}, signal=${signal})\n${logs}`);
    while (waiters.length > 0) {
      const waiter = waiters.pop()!;
      clearTimeout(waiter.timer);
      waiter.reject(err);
    }
  });

  const waitForLogMatch = (regex: RegExp, timeoutMs = 15000): Promise<RegExpMatchArray> => {
    const existing = logs.match(regex);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolveMatch, rejectMatch) => {
      const timer = setTimeout(() => {
        const index = waiters.findIndex((w) => w.regex === regex);
        if (index >= 0) waiters.splice(index, 1);
        rejectMatch(new Error(`timed out waiting for ${regex}\n${logs}`));
      }, timeoutMs);
      waiters.push({ regex, resolve: resolveMatch, reject: rejectMatch, timer });
    });
  };

  try {
    // The bootstrap admin key is written to a 0600 file in the data dir (it is
    // no longer printed to the log). It is written before the server starts
    // listening, so by the time the "listening on" line appears the file exists.
    const [keyPathMatch, urlMatch] = await Promise.all([
      waitForLogMatch(/bootstrap admin key written to (\S+)/),
      waitForLogMatch(/listening on (http:\/\/127\.0\.0\.1:(\d+))/),
    ]);
    const bootstrapKey = (await readFile(keyPathMatch[1], 'utf8')).trim();

    return {
      proc,
      dataDir,
      port: Number(urlMatch[2]),
      url: urlMatch[1],
      bootstrapKey,
      logs: () => logs,
      stop: async () => {
        if (proc.exitCode === null) {
          proc.kill('SIGTERM');
          await waitForClose(proc, 5000).catch(async () => {
            if (proc.exitCode === null) proc.kill('SIGKILL');
            await waitForClose(proc, 2000).catch(() => {});
          });
        }
        await rm(dataDir, { recursive: true, force: true });
      },
    };
  } catch (err) {
    if (proc.exitCode === null) proc.kill('SIGKILL');
    await rm(dataDir, { recursive: true, force: true });
    throw err;
  }
}

export async function authedRequestContext(request: APIRequest, server: ServerHandle) {
  return request.newContext({
    baseURL: server.url,
    extraHTTPHeaders: {
      Authorization: `Bearer ${server.bootstrapKey}`,
    },
  });
}

export async function runMP(server: ServerHandle, args: string[]): Promise<{ stdout: string; stderr: string }> {
  const bin = process.env.MP_BIN;
  const command = bin || 'go';
  const commandArgs = bin ? args : ['run', './cmd/mp', ...args];

  return new Promise((resolveRun, rejectRun) => {
    execFile(command, commandArgs, {
      cwd: repoRoot,
      env: {
        ...process.env,
        MP_API_URL: server.url,
        MP_API_KEY: server.bootstrapKey,
      },
      timeout: 30000,
    }, (err, stdout, stderr) => {
      if (err) {
        rejectRun(new Error(`${command} ${commandArgs.join(' ')} failed\n${stdout}\n${stderr}`));
        return;
      }
      resolveRun({ stdout, stderr });
    });
  });
}

function waitForClose(proc: ChildProcess, timeoutMs: number): Promise<void> {
  if (proc.exitCode !== null) return Promise.resolve();
  return new Promise((resolveClose, rejectClose) => {
    const timer = setTimeout(() => rejectClose(new Error('process did not exit')), timeoutMs);
    proc.once('close', () => {
      clearTimeout(timer);
      resolveClose();
    });
  });
}
