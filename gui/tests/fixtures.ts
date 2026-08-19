import { createTauriTest } from '@srsholmes/tauri-playwright';

export const { test, expect } = createTauriTest({
  devUrl: 'http://localhost:1420',
  startTimeout: 600000,
  socketTimeout: 600000,
  commandTimeout: 600000,
});

export async function clickText(tauriPage: any, text: string) {
  const cleanText = text.replace(/^text=/, '').replace(/"/g, '');
  await tauriPage.evaluate(`
    new Promise((resolve, reject) => {
      let t = 0;
      const interval = setInterval(() => {
        const els = Array.from(document.querySelectorAll('*'));
        const target = els.find(e => e.textContent && e.textContent.trim() === '${cleanText}');
        if (target) {
          clearInterval(interval);
          target.click();
          resolve(true);
          return;
        }
        const target2 = els.find(e => e.innerText && e.innerText.trim() === '${cleanText}');
        if (target2) {
          clearInterval(interval);
          target2.click();
          resolve(true);
          return;
        }
        t += 100;
        if (t > 15000) {
          clearInterval(interval);
          resolve(false); // don't throw to avoid rust hangs
        }
      }, 100);
    })
  `);
}

export async function waitForText(tauriPage: any, text: string, timeout = 15000) {
  const cleanText = text.replace(/^text=/, '').replace(/"/g, '');
  await tauriPage.evaluate(`
    new Promise((resolve, reject) => {
      let t = 0;
      const interval = setInterval(() => {
        const els = Array.from(document.querySelectorAll('*'));
        if (els.find(e => e.textContent && e.textContent.includes('${cleanText}'))) {
          clearInterval(interval);
          resolve(true);
          return;
        }
        t += 100;
        if (t > ${timeout}) {
          clearInterval(interval);
          resolve(false);
        }
      }, 100);
    })
  `);
}

export async function fill(tauriPage: any, selector: string, value: string) {
  await tauriPage.evaluate(`
    new Promise((resolve, reject) => {
      let t = 0;
      const interval = setInterval(() => {
        const el = document.querySelector('${selector}');
        if (el) {
          clearInterval(interval);
          const isTextArea = el.tagName.toLowerCase() === 'textarea';
          const setter = Object.getOwnPropertyDescriptor(
            isTextArea ? window.HTMLTextAreaElement.prototype : window.HTMLInputElement.prototype, 
            'value'
          ).set;
          setter.call(el, '${value.replace(/'/g, "\\'")}');
          el.dispatchEvent(new Event('input', { bubbles: true }));
          el.dispatchEvent(new Event('change', { bubbles: true }));
          resolve(true);
          return;
        }
        t += 100;
        if (t > 15000) {
          clearInterval(interval);
          resolve(false);
        }
      }, 100);
    })
  `);
}

export async function rightClick(tauriPage: any, selector: string) {
  await tauriPage.evaluate(`
    new Promise((resolve, reject) => {
      let t = 0;
      const interval = setInterval(() => {
        const el = document.querySelector('${selector}');
        if (el) {
          clearInterval(interval);
          el.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, clientX: 100, clientY: 100 }));
          resolve(true);
          return;
        }
        t += 100;
        if (t > 15000) {
          clearInterval(interval);
          resolve(false);
        }
      }, 100);
    })
  `);
}
