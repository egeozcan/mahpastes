import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PROJECT_ROOT = path.resolve(__dirname, '../..');
const STUB_DIR = path.join(__dirname, 'desktop-stubs');

// ---------------------------------------------------------------------------
// Keeping test instances out of the developer's desktop
// ---------------------------------------------------------------------------
// MAHPASTES_DATA_DIR isolates the database, but several desktop features
// write or act outside it:
//   - launch at login: ${XDG_CONFIG_HOME:-~/.config}/autostart/mahpastes.desktop
//   - the FUSE mount's sidebar entries: ~/.config/gtk-3.0/bookmarks (XDG_CONFIG_HOME)
//     and ~/.local/share/user-places.xbel (XDG_DATA_HOME)
//   - the FUSE mount point itself (MAHPASTES_MOUNT_DIR)
//   - Open / Open with / Show in folder, which run xdg-open, gio open|launch,
//     gtk-launch and dbus-send (org.freedesktop.FileManager1), starting real
//     applications and file manager windows.
// Every spawned instance therefore gets its own XDG directories next to its
// data dir, and a PATH that resolves those launchers to the stand-ins in
// helpers/desktop-stubs, which only record the call (to launches.log in the
// instance's env dir) and exit 0.

/** Per-instance directory holding the redirected XDG homes and the launch log. */
export function instanceEnvDir(dataDir: string): string {
  return `${dataDir}-env`;
}

/** Lines the desktop stand-ins recorded for an instance, tab-separated argv. */
export function launchLogPath(dataDir: string): string {
  return path.join(instanceEnvDir(dataDir), 'launches.log');
}

/**
 * Environment for a desktop (wails dev) test instance owning `dataDir`.
 * Creates the per-instance XDG directories on first use; a restart reuses them.
 */
export async function desktopInstanceEnv(dataDir: string): Promise<NodeJS.ProcessEnv> {
  // An instance without its own data dir would open the developer's real
  // library (and hand requests to their running app over its socket).
  if (!dataDir || !path.isAbsolute(dataDir) || path.resolve(dataDir).startsWith(os.homedir() + path.sep)) {
    throw new Error(`refusing to start a test instance on data dir ${JSON.stringify(dataDir)}`);
  }
  const envDir = instanceEnvDir(dataDir);
  const xdgConfig = path.join(envDir, 'config');
  const xdgData = path.join(envDir, 'data');
  const xdgState = path.join(envDir, 'state');
  await Promise.all([xdgConfig, xdgData, xdgState].map((d) => fs.mkdir(d, { recursive: true })));

  const extraPath = path.join(os.homedir(), 'go', 'bin');
  return {
    ...process.env,
    MAHPASTES_DATA_DIR: dataDir,
    XDG_CONFIG_HOME: xdgConfig,
    XDG_DATA_HOME: xdgData,
    XDG_STATE_HOME: xdgState,
    MAHPASTES_MOUNT_DIR: path.join(envDir, 'Mahpastes'),
    MAHPASTES_E2E_LAUNCH_LOG: launchLogPath(dataDir),
    PATH: [STUB_DIR, process.env.PATH ?? '', extraPath].join(path.delimiter),
    // Disable public DHT bootstrap peers in tests — mDNS provides same-host
    // discovery and hitting public IPFS nodes is unreliable in CI.
    MAHPASTES_SHARE_DISABLE_WAN_BOOTSTRAP: '1',
    MAHPASTES_START_HIDDEN: '1',
    // Folder-import "delete" moves files to the Trash. Tests create and
    // delete fixtures constantly, so force a permanent remove — otherwise
    // every run silts up the developer's Trash with temp files.
    MAHPASTES_TRASH_MODE: 'remove',
    // StartImportSession normally only accepts a folder the user chose in the
    // native picker, which Playwright cannot drive. This opts that check out
    // so tests can scan a temp directory directly.
    MAHPASTES_ALLOW_UNPICKED_IMPORT: '1',
  };
}

export async function removeInstanceEnv(dataDir: string): Promise<void> {
  await fs.rm(instanceEnvDir(dataDir), { recursive: true, force: true }).catch(() => {});
}

/**
 * The command that runs the Wails CLI. On Linux it goes through
 * scripts/linux/wails.sh, which adds the webkit2_41 build tag WebKitGTK 4.1
 * systems need (without it `wails dev` links the missing 4.0 and the build
 * fails). The script execs wails in place, so the spawned pid stays the
 * `wails dev` process the kill paths expect. Run the suite where the build
 * toolchain is installed (on an image-based host: inside the dev container);
 * from a host without it the script would re-enter the container, and the
 * kill paths could not reach the process.
 */
export async function wailsInvocation(): Promise<{ cmd: string; args: string[] }> {
  if (process.platform === 'linux') {
    return { cmd: 'bash', args: [path.join(PROJECT_ROOT, 'scripts', 'linux', 'wails.sh')] };
  }
  const candidates = [
    path.join(os.homedir(), 'go', 'bin', 'wails'),
    '/usr/local/bin/wails',
    '/usr/local/go/bin/wails',
  ];
  for (const p of candidates) {
    try {
      await fs.access(p);
      return { cmd: p, args: [] };
    } catch {
      // Try next path
    }
  }
  return { cmd: 'wails', args: [] };
}
