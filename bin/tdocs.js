#!/usr/bin/env node

/**
 * tDocs CLI / Server Runner
 * Official Node.js binary wrapper for tDocs
 * Zero external dependencies.
 */

const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const https = require('node:https');
const os = require('node:os');
const path = require('node:path');

// Version comes from package.json (the release workflow rewrites it), so the
// wrapper can never drift from the binary it downloads.
const VERSION = require('../package.json').version;
const BINARY_VERSION = VERSION;

// Map process.platform to tDocs release platform name. Only linux artifacts
// are published; other platforms fall back to a local Go build.
const PLATFORM_MAP = {
  linux: 'linux'
};

// Map process.arch to the GOARCH suffix used in published artifact names.
// GOARCH=arm ships as armv7 (GOARM=7 build) — see scripts/build-release.sh.
const ARCH_MAP = {
  x64: 'amd64',
  arm64: 'arm64',
  ia32: '386',
  arm: 'armv7',
  ppc64: 'ppc64le',
  s390x: 's390x'
};

// TarballName mirrors internal/app.TarballName: tdocs_<ver>_linux_<arch>.tar.gz
function tarballName() {
  const platform = PLATFORM_MAP[process.platform];
  const arch = ARCH_MAP[process.arch];
  if (!platform || !arch) return null;
  return `tdocs_${BINARY_VERSION}_${platform}_${arch}.tar.gz`;
}

function getBinaryName() {
  return process.platform === 'win32' ? 'tdocs.exe' : 'tdocs';
}

function getCacheDir() {
  const custom = process.env.TDOCS_CACHE_DIR || process.env.ROBDOCS_CACHE_DIR || process.env.TELEDRIVE_CACHE_DIR;
  if (custom) return custom;

  if (process.platform === 'win32') {
    return path.join(process.env.LOCALAPPDATA || os.homedir(), 'tdocs', 'bin');
  }
  return path.join(os.homedir(), '.cache', 'tdocs', 'bin');
}

function findLocalBinary() {
  // 1. Explicit environment override
  const override = process.env.TDOCS_BINARY_PATH || process.env.ROBDOCS_BINARY_PATH || process.env.TELEDRIVE_BINARY_PATH;
  if (override && fs.existsSync(override)) {
    return override;
  }

  // 2. Local cache directory
  const cached = path.join(getCacheDir(), `tdocs-v${BINARY_VERSION}${process.platform === 'win32' ? '.exe' : ''}`);
  if (fs.existsSync(cached)) {
    return cached;
  }

  // 3. Local repo root binary (if running inside git clone)
  const repoBin = path.join(__dirname, '..', getBinaryName());
  if (fs.existsSync(repoBin)) {
    return repoBin;
  }

  return null;
}

function downloadUrl(url, destPath) {
  return new Promise((resolve, reject) => {
    const options = {
      headers: {
        'User-Agent': `tdocs-npm-wrapper/${VERSION}`
      }
    };
    if (process.env.GITHUB_TOKEN) {
      options.headers['Authorization'] = `Bearer ${process.env.GITHUB_TOKEN}`;
    }

    const request = https.get(url, options, (res) => {
      // Handle redirects (GitHub Releases redirect to AWS S3/CDN)
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return downloadUrl(res.headers.location, destPath).then(resolve).catch(reject);
      }

      if (res.statusCode !== 200) {
        return reject(new Error(`Server returned HTTP ${res.statusCode} (${res.statusMessage})`));
      }

      const fileStream = fs.createWriteStream(destPath);
      res.pipe(fileStream);
      fileStream.on('finish', () => {
        fileStream.close(() => resolve(destPath));
      });
      fileStream.on('error', (err) => {
        fs.unlink(destPath, () => reject(err));
      });
    });

    request.on('error', (err) => {
      fs.unlink(destPath, () => reject(err));
    });
  });
}

function extractArchive(archivePath, destDir, platform) {
  if (platform === 'windows') {
    // 1. Try Windows tar (tar -xf handles .zip on Windows 10/11)
    const tarResult = spawnSync('tar', ['-xf', archivePath, '-C', destDir], { stdio: 'inherit' });
    if (tarResult.status === 0) return;

    // 2. Fallback to PowerShell Expand-Archive (built into all modern Windows systems)
    const psCmd = `Expand-Archive -LiteralPath '${archivePath}' -DestinationPath '${destDir}' -Force`;
    const psResult = spawnSync('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', psCmd], { stdio: 'inherit' });
    if (psResult.status === 0) return;

    throw new Error(`Failed to extract Windows archive via tar or PowerShell.`);
  }

  // Linux and macOS: tar -xzf
  const extractResult = spawnSync('tar', ['-xzf', archivePath, '-C', destDir], { stdio: 'inherit' });
  if (extractResult.status !== 0) {
    throw new Error(`tar extraction failed with exit code ${extractResult.status}`);
  }
}

async function ensureBinary() {
  const existing = findLocalBinary();
  if (existing) return existing;

  const name = tarballName();
  if (!name) {
    throw new Error(`Unsupported platform or architecture: ${process.platform}-${process.arch} (linux tarballs only; set TDOCS_BINARY_PATH to a local build)`);
  }

  const cacheDir = getCacheDir();
  fs.mkdirSync(cacheDir, { recursive: true });

  const binTarget = path.join(cacheDir, `tdocs-v${BINARY_VERSION}`);
  const downloadArchive = path.join(cacheDir, name);
  const releaseUrl = `https://github.com/robprian/tDocs/releases/download/v${BINARY_VERSION}/${name}`;

  console.log(`[tdocs] Binary not found locally. Downloading tDocs v${BINARY_VERSION} for ${process.platform}/${process.arch}...`);

  try {
    await downloadUrl(releaseUrl, downloadArchive);

    extractArchive(downloadArchive, cacheDir, 'linux');

    // Clean up archive
    try { fs.unlinkSync(downloadArchive); } catch (_) {}

    // In archive, the binary is named 'tdocs' (or 'tdocs.exe')
    const extractedBin = path.join(cacheDir, getBinaryName());
    if (extractedBin !== binTarget && fs.existsSync(extractedBin)) {
      fs.renameSync(extractedBin, binTarget);
    }

    if (process.platform !== 'win32') {
      fs.chmodSync(binTarget, 0o755);
    }

    console.log(`[tdocs] Successfully cached binary at: ${binTarget}`);
    return binTarget;
  } catch (err) {
    // Attempt build fallback if go is installed
    console.warn(`[tdocs] Download failed: ${err.message}`);
    const goCheck = spawnSync('go', ['version'], { stdio: 'pipe' });
    if (goCheck.status === 0) {
      console.log(`[tdocs] Go compiler detected. Attempting to build binary locally...`);
      const repoRoot = path.join(__dirname, '..');
      const buildRes = spawnSync('go', ['build', '-o', binTarget, './cmd/tdocs'], {
        cwd: repoRoot,
        stdio: 'inherit'
      });
      if (buildRes.status === 0 && fs.existsSync(binTarget)) {
        console.log(`[tdocs] Local build succeeded!`);
        return binTarget;
      }
    }

    throw new Error(
      `Could not obtain tDocs binary for ${platform}-${arch}.\n` +
      `URL: ${releaseUrl}\n` +
      `Details: ${err.message}\n\n` +
      `You can manually place the binary at:\n  ${binTarget}\n` +
      `Or set TDOCS_BINARY_PATH=/path/to/tdocs`
    );
  }
}

async function main() {
  try {
    const binPath = await ensureBinary();
    const args = process.argv.slice(2);
    const result = spawnSync(binPath, args, { stdio: 'inherit' });
    process.exit(result.status ?? 0);
  } catch (err) {
    console.error(`[tdocs error] ${err.message}`);
    process.exit(1);
  }
}

main();
