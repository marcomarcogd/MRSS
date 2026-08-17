import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import sharp from 'sharp';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(scriptDir, '..', '..');
const sourcePath = path.join(projectRoot, 'frontend', 'public', 'assets', 'logo.svg');
const outputDir = path.join(projectRoot, 'build', 'windows');
const checkOnly = process.argv.includes('--check');

const appSizes = [16, 20, 24, 32, 40, 48, 64, 128, 256];
const traySizes = [16, 20, 24, 28, 32];

function optimiseSmallVector(source, { darkMode = false } = {}) {
  let svg = source
    .replace('stroke-width="54"', 'stroke-width="64"')
    .replaceAll('stroke-width="20"', 'stroke-width="24"')
    .replace('r="17"', 'r="19"');

  if (darkMode) {
    svg = svg.replace(
      '<rect x="16" y="16" width="480" height="480" rx="112" fill="url(#background)"/>',
      '<rect x="20" y="20" width="472" height="472" rx="108" fill="url(#background)" stroke="#52E4D3" stroke-opacity="0.72" stroke-width="12"/>'
    );
  }

  return svg;
}

async function renderFrame(svg, size, { small = false } = {}) {
  const padding = small ? 0 : Math.max(0, Math.round(size * 0.035));
  const innerSize = size - padding * 2;
  let pipeline = sharp(Buffer.from(svg)).resize(innerSize, innerSize, {
    fit: 'contain',
    kernel: sharp.kernel.lanczos3,
    background: { r: 0, g: 0, b: 0, alpha: 0 },
  });

  if (padding > 0) {
    pipeline = pipeline.extend({
      top: padding,
      bottom: padding,
      left: padding,
      right: padding,
      background: { r: 0, g: 0, b: 0, alpha: 0 },
    });
  }

  const png = await pipeline.png({ compressionLevel: 9, palette: false }).toBuffer();
  await validatePng(png, size);
  return png;
}

async function validatePng(png, expectedSize) {
  const image = sharp(png);
  const metadata = await image.metadata();
  if (
    metadata.width !== expectedSize ||
    metadata.height !== expectedSize ||
    metadata.channels !== 4 ||
    !metadata.hasAlpha
  ) {
    throw new Error(
      `Invalid ${expectedSize}px icon: ${metadata.width}x${metadata.height}, channels=${metadata.channels}, alpha=${metadata.hasAlpha}`
    );
  }

  const { data, info } = await image.ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const alphaAt = (x, y) => data[(y * info.width + x) * info.channels + 3];
  const last = expectedSize - 1;
  if ([alphaAt(0, 0), alphaAt(last, 0), alphaAt(0, last), alphaAt(last, last)].some(Boolean)) {
    throw new Error(`${expectedSize}px icon must have fully transparent corners`);
  }
}

function packIco(frames) {
  const headerSize = 6 + frames.length * 16;
  const header = Buffer.alloc(headerSize);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(frames.length, 4);

  let offset = headerSize;
  frames.forEach(({ size, png }, index) => {
    const entry = 6 + index * 16;
    header.writeUInt8(size === 256 ? 0 : size, entry);
    header.writeUInt8(size === 256 ? 0 : size, entry + 1);
    header.writeUInt8(0, entry + 2);
    header.writeUInt8(0, entry + 3);
    header.writeUInt16LE(1, entry + 4);
    header.writeUInt16LE(32, entry + 6);
    header.writeUInt32LE(png.length, entry + 8);
    header.writeUInt32LE(offset, entry + 12);
    offset += png.length;
  });

  return Buffer.concat([header, ...frames.map(({ png }) => png)]);
}

async function createIco(source, sizes, options = {}) {
  const frames = [];
  for (const size of sizes) {
    const useSmallVector = options.tray || size <= 40;
    const svg = useSmallVector
      ? optimiseSmallVector(source, { darkMode: options.darkMode })
      : source;
    frames.push({ size, png: await renderFrame(svg, size, { small: useSmallVector }) });
  }
  return packIco(frames);
}

function parseIcoFrames(buffer) {
  if (buffer.length < 6 || buffer.readUInt16LE(0) !== 0 || buffer.readUInt16LE(2) !== 1) {
    throw new Error('Invalid ICO header');
  }
  const count = buffer.readUInt16LE(4);
  return Array.from({ length: count }, (_, index) => {
    const entry = 6 + index * 16;
    const width = buffer.readUInt8(entry);
    const height = buffer.readUInt8(entry + 1);
    const bpp = buffer.readUInt16LE(entry + 6);
    if (bpp !== 32) throw new Error(`ICO frame ${index} is ${bpp}bpp instead of 32bpp`);
    const resolvedWidth = width === 0 ? 256 : width;
    const resolvedHeight = height === 0 ? 256 : height;
    if (resolvedWidth !== resolvedHeight) throw new Error(`ICO frame ${index} is not square`);
    const length = buffer.readUInt32LE(entry + 8);
    const offset = buffer.readUInt32LE(entry + 12);
    if (offset < 6 + count * 16 || length === 0 || offset + length > buffer.length) {
      throw new Error(`ICO frame ${index} points outside the file`);
    }
    return { size: resolvedWidth, png: buffer.subarray(offset, offset + length) };
  });
}

async function validateVisualMatch(filename, currentFrames, expectedFrames) {
  for (let index = 0; index < expectedFrames.length; index += 1) {
    const current = currentFrames[index];
    const expected = expectedFrames[index];
    await validatePng(current.png, current.size);

    if (current.png.equals(expected.png)) continue;

    const currentRaw = await sharp(current.png).ensureAlpha().raw().toBuffer();
    const expectedRaw = await sharp(expected.png).ensureAlpha().raw().toBuffer();
    if (currentRaw.length !== expectedRaw.length) {
      throw new Error(`${filename} ${current.size}px frame has an unexpected pixel count`);
    }

    let totalDelta = 0;
    let materiallyDifferentPixels = 0;
    for (let pixel = 0; pixel < currentRaw.length; pixel += 4) {
      let pixelDelta = 0;
      for (let channel = 0; channel < 4; channel += 1) {
        const delta = Math.abs(currentRaw[pixel + channel] - expectedRaw[pixel + channel]);
        totalDelta += delta;
        pixelDelta = Math.max(pixelDelta, delta);
      }
      if (pixelDelta > 8) materiallyDifferentPixels += 1;
    }

    // SVG rasterisation and PNG compression can vary slightly across libvips platforms.
    // Compare decoded pixels with a tight tolerance instead of requiring byte-identical PNGs.
    const meanChannelDelta = totalDelta / currentRaw.length;
    const materialDifferenceRatio = materiallyDifferentPixels / (currentRaw.length / 4);
    if (meanChannelDelta > 1.5 || materialDifferenceRatio > 0.02) {
      throw new Error(
        `${filename} ${current.size}px frame is stale ` +
          `(mean delta ${meanChannelDelta.toFixed(3)}, changed pixels ${(materialDifferenceRatio * 100).toFixed(2)}%)`
      );
    }
  }
}

async function persistOrCheck(filename, expected, expectedSizes) {
  const target = path.join(outputDir, filename);
  if (checkOnly) {
    const current = await readFile(target);
    const currentFrames = parseIcoFrames(current);
    const expectedFrames = parseIcoFrames(expected);
    const actualSizes = currentFrames.map(({ size }) => size);
    if (actualSizes.join(',') !== expectedSizes.join(',')) {
      throw new Error(
        `${filename} sizes are ${actualSizes.join(',')}, expected ${expectedSizes.join(',')}`
      );
    }
    await validateVisualMatch(filename, currentFrames, expectedFrames);
    return;
  }
  await writeFile(target, expected);
}

const source = await readFile(sourcePath, 'utf8');
await mkdir(outputDir, { recursive: true });

const appIco = await createIco(source, appSizes);
const trayLightIco = await createIco(source, traySizes, { tray: true });
const trayDarkIco = await createIco(source, traySizes, { tray: true, darkMode: true });

await persistOrCheck('icon.ico', appIco, appSizes);
await persistOrCheck('tray-light.ico', trayLightIco, traySizes);
await persistOrCheck('tray-dark.ico', trayDarkIco, traySizes);

console.log(
  `${checkOnly ? 'Validated' : 'Generated'} Windows icons: app=${appSizes.join('/')} tray=${traySizes.join('/')}`
);
