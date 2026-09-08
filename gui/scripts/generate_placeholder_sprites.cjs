const fs = require('fs');
const { createCanvas } = require('canvas');

// We'll mimic a small sprite sheet: 3 columns, 5 rows.
// Let's say each frame is 64x64. Total: 192x320
const FRAME_W = 64;
const FRAME_H = 64;
const COLS = 3;
const ROWS = 5;
const WIDTH = COLS * FRAME_W;
const HEIGHT = ROWS * FRAME_H;

function createLayer(filename, drawFunc) {
  const canvas = createCanvas(WIDTH, HEIGHT);
  const ctx = canvas.getContext('2d');
  
  for (let r = 0; r < ROWS; r++) {
    for (let c = 0; c < COLS; c++) {
      ctx.save();
      ctx.translate(c * FRAME_W, r * FRAME_H);
      drawFunc(ctx, c, r);
      ctx.restore();
    }
  }

  const out = fs.createWriteStream(__dirname + '/../public/' + filename);
  const stream = canvas.createPNGStream();
  stream.pipe(out);
  out.on('finish', () => console.log('Created ' + filename));
}

// 1. Body Base (Skin color oval)
createLayer('sprite_body.png', (ctx, c, r) => {
  ctx.fillStyle = '#ffccaa'; // skin tone
  ctx.beginPath();
  // simple capsule shape
  ctx.roundRect(16, 16, 32, 40, 10);
  ctx.fill();
  
  // simple eyes
  ctx.fillStyle = 'black';
  ctx.fillRect(24, 24, 4, 4);
  ctx.fillRect(36, 24, 4, 4);
});

// 2. Hair (Brown/Black top)
createLayer('sprite_hair_1.png', (ctx, c, r) => {
  ctx.fillStyle = '#4a2f1d'; // brown hair
  ctx.beginPath();
  ctx.roundRect(14, 12, 36, 16, 8);
  ctx.fill();
});

// 3. Clothes (Blue shirt)
createLayer('sprite_clothes_1.png', (ctx, c, r) => {
  ctx.fillStyle = '#3b82f6'; // blue
  ctx.beginPath();
  ctx.roundRect(14, 32, 36, 20, 4);
  ctx.fill();
});

// Clothes variation (Red shirt)
createLayer('sprite_clothes_2.png', (ctx, c, r) => {
  ctx.fillStyle = '#ef4444'; // red
  ctx.beginPath();
  ctx.roundRect(14, 32, 36, 20, 4);
  ctx.fill();
});
