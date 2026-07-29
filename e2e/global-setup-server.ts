import { ensureServerBinary } from './helpers/server-binary.js';

// The server suite drives the headless mahpastesd binary directly — no Wails
// instances — so all it needs up front is a current build of that binary.
async function globalSetup(): Promise<void> {
  const bin = await ensureServerBinary();
  console.log(`\n🚀 Using mahpastesd at ${bin}\n`);
}

export default globalSetup;
