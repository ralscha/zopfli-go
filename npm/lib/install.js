'use strict';

const fs = require('node:fs');
const path = require('node:path');
const https = require('node:https');

const ROOT_DIR = path.resolve(__dirname, '..', '..');
const PACKAGE_JSON_PATH = path.join(ROOT_DIR, 'package.json');
const NATIVE_DIR = path.join(ROOT_DIR, 'npm', 'native');

async function installBinary() {
  if (process.env.ZOPFLI_GO_SKIP_DOWNLOAD === '1') {
    return;
  }

  const packageJson = JSON.parse(fs.readFileSync(PACKAGE_JSON_PATH, 'utf8'));
  const target = resolveTarget();
  const binaryFileName = `${packageJson.zopfliGo.binaryName}${target.extension}`;
  const destination = path.join(NATIVE_DIR, binaryFileName);

  fs.mkdirSync(NATIVE_DIR, { recursive: true });

  const url = process.env.ZOPFLI_GO_DOWNLOAD_URL || buildDownloadUrl(packageJson, target);
  const temporaryFile = `${destination}.download`;
  await download(url, temporaryFile);

  fs.renameSync(temporaryFile, destination);
  if (process.platform !== 'win32') {
    fs.chmodSync(destination, 0o755);
  }
}

function resolveTarget() {
  const platformMap = {
    darwin: 'darwin',
    linux: 'linux',
    win32: 'windows',
  };
  const archMap = {
    arm64: 'arm64',
    x64: 'amd64',
  };

  const os = platformMap[process.platform];
  if (!os) {
    throw new Error(`unsupported platform: ${process.platform}`);
  }

  const arch = archMap[process.arch];
  if (!arch) {
    throw new Error(`unsupported architecture: ${process.arch}`);
  }

  return {
    arch,
    extension: process.platform === 'win32' ? '.exe' : '',
    os,
  };
}

function buildDownloadUrl(packageJson, target) {
  const config = packageJson.zopfliGo;
  const version = packageJson.version;
  const tag = `${config.tagPrefix || ''}${version}`;
  const assetName = renderTemplate(config.assetNameTemplate, {
    arch: target.arch,
    binary: config.binaryName,
    extension: target.extension,
    os: target.os,
    version,
  });

  return `https://github.com/${config.owner}/${config.repo}/releases/download/${tag}/${assetName}`;
}

function renderTemplate(template, values) {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key) => {
    if (!(key in values)) {
      throw new Error(`unknown template key: ${key}`);
    }
    return values[key];
  });
}

function download(url, destination) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(destination);

    const fail = (error) => {
      file.destroy();
      fs.rmSync(destination, { force: true });
      reject(error);
    };

    const request = https.get(url, (response) => {
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        file.close(() => {
          fs.rmSync(destination, { force: true });
          download(response.headers.location, destination).then(resolve, reject);
        });
        return;
      }

      if (response.statusCode !== 200) {
        response.resume();
        fail(new Error(`download failed: ${response.statusCode} ${response.statusMessage || ''} (${url})`));
        return;
      }

      response.pipe(file);
      file.on('finish', () => {
        file.close((closeError) => {
          if (closeError) {
            fail(closeError);
            return;
          }
          resolve();
        });
      });
    });

    request.on('error', fail);
    file.on('error', fail);
  });
}

module.exports = {
  buildDownloadUrl,
  installBinary,
  renderTemplate,
  resolveTarget,
};