const { id, name } = window.MATCH;
const canvas = document.getElementById('game');
const ctx = canvas.getContext('2d');
const hud = document.getElementById('status');
const TICK_MS = 1000 / 30;
const lerp = (a, b, t) => a + (b - a) * t;

const keys = new Set();
const mouse = { x: canvas.width / 2, y: canvas.height / 2, down: false };
addEventListener('keydown', e => keys.add(e.key.toLowerCase()));
addEventListener('keyup', e => keys.delete(e.key.toLowerCase()));
canvas.addEventListener('mousemove', e => {
  const r = canvas.getBoundingClientRect();
  mouse.x = (e.clientX - r.left) * canvas.width / r.width;
  mouse.y = (e.clientY - r.top) * canvas.height / r.height;
});
canvas.addEventListener('mousedown', () => (mouse.down = true));
addEventListener('mouseup', () => (mouse.down = false));

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
  else if (msg.over) hud.textContent = `Winner: ${msg.winner}`;
  else if (msg.me.alive) hud.textContent = `Alive: ${msg.alive} · HP ${Math.round(msg.me.hp)}`;
  else hud.textContent = `You died · ${msg.alive} alive`;
};

// Send input at the server's tick rate. Aim is relative to the screen center,
// which is where the camera keeps the player.
setInterval(() => {
  if (ws.readyState !== WebSocket.OPEN) return;
  const dx = (keys.has('d') || keys.has('arrowright') ? 1 : 0) - (keys.has('a') || keys.has('arrowleft') ? 1 : 0);
  const dy = (keys.has('s') || keys.has('arrowdown') ? 1 : 0) - (keys.has('w') || keys.has('arrowup') ? 1 : 0);
  const aim = Math.atan2(mouse.y - canvas.height / 2, mouse.x - canvas.width / 2);
  ws.send(JSON.stringify({ t: 'input', dx, dy, aim, shoot: mouse.down }));
}, TICK_MS);

function drawPlayer(x, y, color) {
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(x, y, 12, 0, Math.PI * 2);
  ctx.fill();
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

  if (curr.started && !curr.me.alive) {
    ctx.fillStyle = '#fff';
    ctx.font = '32px sans-serif';
    ctx.textAlign = 'center';
    ctx.fillText('You died', canvas.width / 2, canvas.height / 2);
  }
}
requestAnimationFrame(render);
