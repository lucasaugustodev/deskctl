/**
 * Figma bridge — talks to figma-cli's speed daemon via HTTP.
 *
 * The daemon must be running (via `figma-cli connect`).
 * This bridge just translates stdin JSON → daemon HTTP calls.
 */

import { createInterface } from 'readline';
import { readFileSync, existsSync } from 'fs';
import { join } from 'path';
import { homedir } from 'os';

const DAEMON_PORT = 3456;
const TOKEN_FILE = join(homedir(), '.figma-ds-cli', '.daemon-token');

function getToken() {
  if (!existsSync(TOKEN_FILE)) return null;
  return readFileSync(TOKEN_FILE, 'utf8').trim();
}

async function daemonCall(action, code = '', timeout = 30000) {
  const token = getToken();
  if (!token) throw new Error('No daemon token. Run: cd figma-cli && node src/index.js connect');

  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);

  try {
    const resp = await fetch(`http://localhost:${DAEMON_PORT}/exec`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Daemon-Token': token,
      },
      body: JSON.stringify({ action, code }),
      signal: controller.signal,
    });
    const data = await resp.json();
    if (data.error) throw new Error(data.error);
    return data.result ?? data;
  } finally {
    clearTimeout(timer);
  }
}

async function daemonHealth() {
  const token = getToken();
  try {
    const resp = await fetch(`http://localhost:${DAEMON_PORT}/health`, {
      headers: { 'X-Daemon-Token': token || '' },
    });
    return await resp.json();
  } catch {
    return { status: 'offline' };
  }
}

async function handleCommand(cmd) {
  try {
    switch (cmd.cmd) {
      case 'status': {
        const h = await daemonHealth();
        return { ok: true, result: h };
      }

      case 'eval': {
        const r = await daemonCall('eval', cmd.code);
        return { ok: true, result: r };
      }

      case 'canvas-info': {
        const r = await daemonCall('eval',
          'JSON.stringify({page:figma.currentPage.name,children:figma.currentPage.children.length,fileKey:figma.fileKey})');
        return { ok: true, result: r };
      }

      case 'tree': {
        const depth = cmd.depth || 3;
        const r = await daemonCall('eval', `
          (function(){
            function w(n,d,m){
              var o={name:n.name,type:n.type,id:n.id};
              if(n.x!==undefined){o.x=Math.round(n.x);o.y=Math.round(n.y)}
              if(n.width!==undefined){o.w=Math.round(n.width);o.h=Math.round(n.height)}
              if(n.characters)o.text=n.characters.slice(0,80);
              if(d<m&&n.children)o.children=n.children.map(function(c){return w(c,d+1,m)});
              return o;
            }
            return JSON.stringify(w(figma.currentPage,0,${depth}));
          })()
        `);
        return { ok: true, result: r };
      }

      case 'create-frame': {
        const { name, x, y, w, h, fill } = cmd;
        const fh = fill || '#0D0D12';
        const r = await daemonCall('eval', `
          (function(){
            var f=figma.createFrame();
            f.name=${JSON.stringify(name||'Frame')};
            f.resize(${w||1080},${h||1350});
            f.x=${x||0};f.y=${y||0};
            f.fills=[{type:'SOLID',color:{r:${parseInt(fh.slice(1,3),16)/255},g:${parseInt(fh.slice(3,5),16)/255},b:${parseInt(fh.slice(5,7),16)/255}}}];
            figma.currentPage.appendChild(f);
            return JSON.stringify({id:f.id,name:f.name,x:f.x,y:f.y});
          })()
        `);
        return { ok: true, result: r };
      }

      case 'create-text': {
        const { text, x, y, size, font, style, fill, parent } = cmd;
        const fh = fill || '#FFFFFF';
        const r = await daemonCall('eval', `
          (async function(){
            await figma.loadFontAsync({family:${JSON.stringify(font||'Inter')},style:${JSON.stringify(style||'Bold')}});
            var t=figma.createText();
            t.fontName={family:${JSON.stringify(font||'Inter')},style:${JSON.stringify(style||'Bold')}};
            t.fontSize=${size||48};
            t.characters=${JSON.stringify(text||'Text')};
            t.fills=[{type:'SOLID',color:{r:${parseInt(fh.slice(1,3),16)/255},g:${parseInt(fh.slice(3,5),16)/255},b:${parseInt(fh.slice(5,7),16)/255}}}];
            t.x=${x||0};t.y=${y||0};
            ${parent?`var p=figma.getNodeById(${JSON.stringify(parent)});if(p)p.appendChild(t);`:''}
            return JSON.stringify({id:t.id,text:t.characters.slice(0,50)});
          })()
        `);
        return { ok: true, result: r };
      }

      case 'set-text': {
        const r = await daemonCall('eval', `
          (async function(){
            var n=figma.getNodeById(${JSON.stringify(cmd.id)});
            if(!n)return JSON.stringify({error:'not found'});
            await figma.loadFontAsync(n.fontName);
            n.characters=${JSON.stringify(cmd.text)};
            return JSON.stringify({id:n.id,text:n.characters.slice(0,50)});
          })()
        `);
        return { ok: true, result: r };
      }

      case 'move': {
        const r = await daemonCall('eval', `
          (function(){
            var n=figma.getNodeById(${JSON.stringify(cmd.id)});
            if(!n)return JSON.stringify({error:'not found'});
            ${cmd.x !== undefined ? `n.x=${cmd.x};` : ''}
            ${cmd.y !== undefined ? `n.y=${cmd.y};` : ''}
            return JSON.stringify({id:n.id,x:Math.round(n.x),y:Math.round(n.y)});
          })()
        `);
        return { ok: true, result: r };
      }

      case 'delete': {
        const r = await daemonCall('eval', `
          (function(){
            var n=figma.getNodeById(${JSON.stringify(cmd.id)});
            if(!n)return JSON.stringify({error:'not found'});
            var nm=n.name;n.remove();
            return JSON.stringify({deleted:nm});
          })()
        `);
        return { ok: true, result: r };
      }

      default:
        return { ok: false, error: `Unknown: ${cmd.cmd}` };
    }
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

// Main
process.stderr.write('[figma-bridge] daemon on port ' + DAEMON_PORT + '\n');

const rl = createInterface({ input: process.stdin });
for await (const line of rl) {
  const t = line.trim();
  if (!t || t === 'exit') break;
  let cmd;
  try { cmd = JSON.parse(t); } catch { process.stdout.write('{"ok":false,"error":"bad json"}\n'); continue; }
  const result = await handleCommand(cmd);
  process.stdout.write(JSON.stringify(result) + '\n');
}
process.exit(0);
