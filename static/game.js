const { id, name } = window.MATCH;
const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');
const hud = document.getElementById('status');
const TICK_MS = 1000 / 30;
const lerp = (a, b, t) => a + (b - a) * t;

// Touch and mouse both go through Pointer Events. Each pointer is assigned a role:
//  - 'stick': movement joystick (bottom-left)
//  - 'fire':  fire button (bottom-right)
//  - 'aim':   anywhere else, sets the aim point
const STICK = { x: 110, y: canvas.height - 110, r: 70, hit: 90 };
const FIRE = { x: canvas.width - 110, y: canvas.height - 110, r: 55, hit: 65 };

const keys = new Set();
const mouse = { x: canvas.width / 2, y: canvas.height / 2, down: false };
const stick = { dx: 0, dy: 0 };
let fireHeld = false;
let touchActive = false;
const pointers = new Map(); // pointerId -> role

addEventListener('keydown', e => keys.add(e.key.toLowerCase()));
addEventListener('keyup', e => keys.delete(e.key.toLowerCase()));

canvas.style.touchAction = 'none'; // stop the page from scrolling while playing

function canvasPos(e) {
  const r = canvas.getBoundingClientRect();
  return {
    x: (e.clientX - r.left) * canvas.width / r.width,
    y: (e.clientY - r.top) * canvas.height / r.height,
  };
}

const dist = (a, b) => Math.hypot(a.x - b.x, a.y - b.y);

function updateStick(p) {
  const vx = p.x - STICK.x;
  const vy = p.y - STICK.y;
  const len = Math.hypot(vx, vy);
  const k = len > STICK.r ? STICK.r / len : 1;
  stick.dx = (vx * k) / STICK.r;
  stick.dy = (vy * k) / STICK.r;
}

canvas.addEventListener('pointerdown', e => {
  canvas.setPointerCapture(e.pointerId);
  const p = canvasPos(e);
  touchActive = touchActive || e.pointerType === 'touch';
  let role;
  if (dist(p, STICK) <= STICK.hit && touchActive) {
    role = 'stick';
    updateStick(p);
  } else if (dist(p, FIRE) <= FIRE.hit && touchActive) {
    role = 'fire';
    fireHeld = true;
  } else {
    role = 'aim';
    mouse.x = p.x;
    mouse.y = p.y;
    if (e.pointerType === 'mouse') mouse.down = true;
  }
  pointers.set(e.pointerId, role);
});

canvas.addEventListener('pointermove', e => {
  const p = canvasPos(e);
  const role = pointers.get(e.pointerId);
  if (role === 'stick') updateStick(p);
  if (role === 'aim' || (!role && e.pointerType === 'mouse')) {
    mouse.x = p.x;
    mouse.y = p.y;
  }
});

function endPointer(e) {
  const role = pointers.get(e.pointerId);
  pointers.delete(e.pointerId);
  if (role === 'stick') { stick.dx = 0; stick.dy = 0; }
  if (role === 'fire') {
    fireHeld = [...pointers.values()].includes('fire');
  }
  if (role === 'aim' && e.pointerType === 'mouse') mouse.down = false;
}
canvas.addEventListener('pointerup', endPointer);
canvas.addEventListener('pointercancel', endPointer);
canvas.addEventListener('pointerleave', e => {
  if (e.pointerType === 'mouse') { mouse.down = false; }
});

const proto = location.protocol === 'https:' ? 'wss' : 'ws';
const ws = new WebSocket(`${proto}://${location.host}/ws/${id}?name=${encodeURIComponent(name)}`);

let prev = null, curr = null, currAt = 0;
ws.onmessage = e => {
  const msg = JSON.parse(e.data);
  if (msg.t !== 'state') return;
  prev = curr ?? msg;
  curr = msg;
  currAt = performance.now();

  if (!msg.started) hud.textContent = `Waiting for players (${msg.alive}), need 2+ to start`;
  else if (msg.over) hud.textContent = msg.me.place === 1 ? 'Victory Royale' : `Winner: ${msg.winner}`;
  else if (msg.me.alive) hud.textContent = `Alive: ${msg.alive} · HP ${Math.round(msg.me.hp)}`;
  else hud.textContent = `Eliminated · placed #${msg.me.place}`;
};

// Send input at the server's tick rate. Aim is relative to the screen center,
// which is where the camera keeps the player.
setInterval(() => {
  if (ws.readyState !== WebSocket.OPEN) return;
  const left = keys.has('a') || keys.has('arrowleft');
  const right = keys.has('d') || keys.has('arrowright');
  const up = keys.has('w') || keys.has('arrowup');
  const down = keys.has('s') || keys.has('arrowdown');
  const kx = (right ? 1 : 0) - (left ? 1 : 0);
  const ky = (down ? 1 : 0) - (up ? 1 : 0);
  // Keyboard and joystick are combined, then clamped to the unit square.
  const dx = Math.max(-1, Math.min(1, kx + stick.dx));
  const dy = Math.max(-1, Math.min(1, ky + stick.dy));
  const aim = Math.atan2(mouse.y - canvas.height / 2, mouse.x - canvas.width / 2);
  const shoot = mouse.down || fireHeld || keys.has(' ');
  ws.send(JSON.stringify({ t: 'input', dx, dy, aim, shoot }));
}, TICK_MS);

function drawPlayer(x, y, color) {
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(x, y, 12, 0, Math.PI * 2);
  ctx.fill();
}

function drawHUDText(lines) {
  ctx.font = '14px sans-serif';
  ctx.textAlign = 'right';
  ctx.fillStyle = '#fff';
  lines.forEach((line, i) => ctx.fillText(line, canvas.width - 10, 20 + i * 18));
}

// On-screen controls. Drawn only after the first touch so desktop stays clean.
function drawTouchControls() {
  ctx.save();
  ctx.globalAlpha = 0.35;
  ctx.fillStyle = '#000';
  ctx.beginPath();
  ctx.arc(STICK.x, STICK.y, STICK.r, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = '#fff';
  ctx.beginPath();
  ctx.arc(STICK.x + stick.dx * STICK.r, STICK.y + stick.dy * STICK.r, 26, 0, Math.PI * 2);
  ctx.fill();

  ctx.globalAlpha = fireHeld ? 0.7 : 0.35;
  ctx.fillStyle = '#ef5350';
  ctx.beginPath();
  ctx.arc(FIRE.x, FIRE.y, FIRE.r, 0, Math.PI * 2);
  ctx.fill();
  ctx.globalAlpha = 1;
  ctx.fillStyle = '#fff';
  ctx.font = 'bold 16px sans-serif';
  ctx.textAlign = 'center';
  ctx.fillText('FIRE', FIRE.x, FIRE.y + 6);
  ctx.restore();
}

function render(now) {
  requestAnimationFrame(render);
  ctx.fillStyle = '#1b2a1b';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  if (!curr) return;

  const t = Math.min(1, (now - currAt) / TICK_MS);
  const meX = lerp(prev.me.x, curr.me.x, t);
  const meY = lerp(prev.me.y, curr.me.y, t);

  ctx.save();
  ctx.translate(canvas.width / 2 - meX, canvas.height / 2 - meY);

  ctx.strokeStyle = '#444';
  ctx.lineWidth = 4;
  ctx.strokeRect(0, 0, 2000, 2000);

  ctx.fillStyle = '#5b5b5b';
  for (const r of curr.obstacles) ctx.fillRect(r.x, r.y, r.w, r.h);

  ctx.fillStyle = '#f5c542';
  for (const l of curr.loot) ctx.fillRect(l.x - 5, l.y - 5, 10, 10);

  // Darken everything outside the zone: a big rect with a circular hole (evenodd).
  const z = curr.zone;
  ctx.fillStyle = 'rgba(180, 0, 0, 0.25)';
  ctx.beginPath();
  ctx.rect(-5000, -5000, 10000, 10000);
  ctx.moveTo(z.x + z.r, z.y);
  ctx.arc(z.x, z.y, z.r, 0, Math.PI * 2);
  ctx.fill('evenodd');

  const prevById = new Map(prev.players.map(p => [p.id, p]));
  for (const p of curr.players) {
    const old = prevById.get(p.id) ?? p;
    drawPlayer(lerp(old.x, p.x, t), lerp(old.y, p.y, t), '#ef5350');
  }
  drawPlayer(meX, meY, curr.me.alive ? '#4fc3f7' : '#607d8b');

  const aim = Math.atan2(mouse.y - canvas.height / 2, mouse.x - canvas.width / 2);
  ctx.strokeStyle = '#fff';
  ctx.beginPath();
  ctx.moveTo(meX, meY);
  ctx.lineTo(meX + Math.cos(aim) * 40, meY + Math.sin(aim) * 40);
  ctx.stroke();
  ctx.restore();

  drawHUDText([`Alive: ${curr.alive}`, ...curr.events]);
  if (touchActive) drawTouchControls();

  ctx.textAlign = 'center';
  if (curr.started && !curr.me.alive && !curr.over) {
    ctx.fillStyle = '#fff';
    ctx.font = '32px sans-serif';
    ctx.fillText(`Eliminated — #${curr.me.place}`, canvas.width / 2, canvas.height / 2);
  }
  if (curr.over) {
    ctx.fillStyle = 'rgba(0, 0, 0, 0.55)';
    ctx.fillRect(0, canvas.height / 2 - 60, canvas.width, 120);
    ctx.fillStyle = '#ffd54f';
    ctx.font = 'bold 36px sans-serif';
    const title = curr.me.place === 1 ? 'Victory Royale' : `${curr.winner} wins`;
    ctx.fillText(title, canvas.width / 2, canvas.height / 2);
    ctx.fillStyle = '#fff';
    ctx.font = '18px sans-serif';
    ctx.fillText(`You placed #${curr.me.place}`, canvas.width / 2, canvas.height / 2 + 30);
  }
}
requestAnimationFrame(render);
