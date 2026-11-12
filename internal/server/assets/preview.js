(() => {
  'use strict';

  const mermaidURL = document.currentScript.dataset.mermaid;
  const katexURL = document.currentScript.dataset.katex;
  const asciidoctorURL = document.currentScript.dataset.asciidoctor;
  const content = document.getElementById('content');
  const baseTitle = document.title;
  let lines = false;
  let cursor = { line: 1, total: 1 };

  // A diagram is drawn again only when its source changed: mermaid is slow,
  // and redrawing every one on each keystroke would make the page flicker.
  const diagrams = new Map();
  let mermaidLoad = null;

  const formulas = new Map();
  let katexLoad = null;

  let asciidoctorLoad = null;
  let renderSeq = 0;

  const highlighted = new Map();

  const absolute = /^(?:[a-z][a-z0-9+.-]*:|\/|#)/i;

  async function render(data) {
    const seq = ++renderSeq;
    const tmpl = document.createElement('template');
    if (data.asciidoc !== undefined) {
      try {
        tmpl.innerHTML = await convertAsciiDoc(data.asciidoc);
      } catch (e) {
        const pre = document.createElement('pre');
        pre.className = 'goda-preview-error';
        pre.textContent = String((e && e.stack) || e);
        tmpl.content.replaceChildren(pre);
      }
      await highlightCode(tmpl.content);
      // The conversion is asynchronous: a later render may have been
      // shown meanwhile.
      if (seq !== renderSeq) {
        return;
      }
      sourceLines(tmpl.content);
      markTeX(tmpl.content);
    } else {
      tmpl.innerHTML = data.html;
    }
    // Relative to the document, not to the page. Rewritten before the nodes
    // are attached, so that the browser never requests the wrong URL.
    for (const el of tmpl.content.querySelectorAll('img[src], video[src], audio[src], source[src]')) {
      const src = el.getAttribute('src');
      if (!absolute.test(src)) {
        el.setAttribute('src', 'files/' + src);
      }
    }
    lines = data.lines;
    document.title = data.title || baseTitle;
    // Before the nodes are attached, so that formulas already drawn never
    // show as TeX for a moment.
    const math = drawMath(tmpl.content);
    content.replaceChildren(tmpl.content);
    Promise.allSettled([drawDiagrams(), math]).finally(scroll);
    scroll();
  }

  function loadAsciidoctor() {
    if (!asciidoctorLoad) {
      asciidoctorLoad = import(asciidoctorURL).catch((e) => {
        asciidoctorLoad = null;
        throw e;
      });
    }
    return asciidoctorLoad;
  }

  async function convertAsciiDoc(source) {
    if (!asciidoctorURL) {
      throw new Error('no URL of asciidoctor.js');
    }
    const asciidoctor = await loadAsciidoctor();
    // secure: no include, and nothing read from files or URLs.
    const doc = await asciidoctor.load(source, {
      safe: 'secure',
      sourcemap: true,
      attributes: { showtitle: '' },
    });
    // A role is the only thing of a block that every converter writes out
    // (as a class); sourceLines turns it into data-line.
    for (const block of doc.findBy((b) => b.getLineNumber() !== undefined)) {
      block.addRole('goda-line-' + block.getLineNumber());
    }
    return doc.convert();
  }

  // The code of AsciiDoc is coloured by the server, with the chroma that
  // colours Markdown's. The page links highlight.css only when it does.
  async function highlightCode(root) {
    if (!document.querySelector('link[href$="highlight.css"]')) {
      return;
    }
    const pending = [];
    // Code with callouts has elements of its own, which colouring would lose.
    for (const el of root.querySelectorAll('pre > code[data-lang]')) {
      if (el.children.length > 0) {
        continue;
      }
      const key = el.dataset.lang + '\0' + el.textContent;
      if (highlighted.has(key)) {
        colorCode(el, highlighted.get(key));
      } else {
        pending.push({ el, key });
      }
    }
    if (pending.length === 0) {
      return;
    }
    let colored;
    try {
      const res = await fetch('../../highlight', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(pending.map((p) => ({ lang: p.el.dataset.lang, code: p.el.textContent }))),
      });
      if (!res.ok) {
        throw new Error('highlight: ' + res.status);
      }
      colored = await res.json();
    } catch (e) {
      console.error(e);
      return;
    }
    if (highlighted.size > 500) {
      highlighted.clear();
    }
    pending.forEach((p, i) => {
      highlighted.set(p.key, colored[i]);
      colorCode(p.el, colored[i]);
    });
  }

  function colorCode(el, html) {
    if (html) {
      el.innerHTML = html;
      el.parentElement.classList.add('chroma');
    }
  }

  const lineRole = /(?:^|\s)goda-line-(\d+)(?=\s|$)/;

  function sourceLines(root) {
    for (const el of root.querySelectorAll('[class*="goda-line-"]')) {
      const m = lineRole.exec(el.className);
      if (m) {
        el.classList.remove('goda-line-' + m[1]);
        el.dataset.line = m[1];
      }
    }
  }

  // asciidoctor.js writes latexmath as \(…\) and \[…\]; they become the
  // elements that Markdown's math is drawn from.
  function markTeX(root) {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode: (n) => n.parentElement && n.parentElement.closest('pre, code, .math')
        ? NodeFilter.FILTER_REJECT
        : NodeFilter.FILTER_ACCEPT,
    });
    const texts = [];
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (n.data.includes('\\(') || n.data.includes('\\[')) {
        texts.push(n);
      }
    }
    const tex = /\\\(([\s\S]+?)\\\)|\\\[([\s\S]+?)\\\]/g;
    for (const n of texts) {
      const parts = [];
      let last = 0;
      for (const m of n.data.matchAll(tex)) {
        const display = m[2] !== undefined;
        const el = document.createElement(display ? 'div' : 'span');
        el.className = display ? 'math math-display' : 'math math-inline';
        el.textContent = display ? m[2] : m[1];
        parts.push(n.data.slice(last, m.index), el);
        last = m.index + m[0].length;
      }
      if (parts.length > 0) {
        n.replaceWith(...parts, n.data.slice(last));
      }
    }
  }

  function loadKaTeX() {
    if (!katexLoad) {
      katexLoad = new Promise((resolve, reject) => {
        if (!document.getElementById('katex-css')) {
          const link = document.createElement('link');
          link.id = 'katex-css';
          link.rel = 'stylesheet';
          link.href = katexURL + 'katex.min.css';
          document.head.appendChild(link);
        }
        const s = document.createElement('script');
        s.src = katexURL + 'katex.min.js';
        s.onload = () => resolve(window.katex);
        s.onerror = () => {
          katexLoad = null;
          reject(new Error('cannot load ' + s.src));
        };
        document.head.appendChild(s);
      });
    }
    return katexLoad;
  }

  // Formulas are cached like the diagrams, though KaTeX is fast: the page
  // is rendered again on every change of the buffer.
  async function drawMath(root) {
    const pending = [];
    for (const el of root.querySelectorAll('.math')) {
      const key = (el.classList.contains('math-display') ? 'D' : 'I') + el.textContent;
      el.dataset.key = key;
      if (formulas.has(key)) {
        el.innerHTML = formulas.get(key);
      } else {
        pending.push(el);
      }
    }
    if (pending.length === 0 || !katexURL) {
      return;
    }
    let katex;
    try {
      katex = await loadKaTeX();
    } catch (e) {
      console.error(e);
      return;
    }
    if (!pending[0].isConnected) {
      return;
    }
    if (formulas.size > 500) {
      formulas.clear();
    }
    for (const el of pending) {
      const html = katex.renderToString(el.textContent, {
        displayMode: el.dataset.key[0] === 'D',
        throwOnError: false,
      });
      formulas.set(el.dataset.key, html);
      el.innerHTML = html;
    }
  }

  function loadMermaid() {
    if (!mermaidLoad) {
      mermaidLoad = new Promise((resolve, reject) => {
        const s = document.createElement('script');
        s.src = mermaidURL;
        s.onload = () => {
          window.mermaid.initialize({ startOnLoad: false });
          resolve(window.mermaid);
        };
        s.onerror = () => {
          mermaidLoad = null;
          reject(new Error('cannot load ' + mermaidURL));
        };
        document.head.appendChild(s);
      });
    }
    return mermaidLoad;
  }

  async function drawDiagrams() {
    const pending = [];
    for (const el of content.querySelectorAll('pre.mermaid')) {
      const src = el.textContent;
      el.dataset.source = src;
      if (diagrams.has(src)) {
        el.innerHTML = diagrams.get(src);
        el.dataset.processed = 'true';
      } else {
        pending.push(el);
      }
    }
    if (pending.length === 0 || !mermaidURL) {
      return;
    }
    let mermaid;
    try {
      mermaid = await loadMermaid();
    } catch (e) {
      console.error(e);
      return;
    }
    if (!pending[0].isConnected) {
      return;
    }
    await mermaid.run({ nodes: pending, suppressErrors: true });
    if (diagrams.size > 100) {
      diagrams.clear();
    }
    for (const el of pending) {
      if (el.querySelector('svg')) {
        diagrams.set(el.dataset.source, el.innerHTML);
      }
    }
  }

  function scroll() {
    const view = window.innerHeight;
    // A window that is not shown yet may have no height; the resize when
    // it is shown scrolls it.
    if (view === 0) {
      return;
    }
    let top;
    if (lines) {
      top = lineTop(cursor.line);
    } else {
      const max = document.documentElement.scrollHeight - view;
      top = max * (cursor.line - 1) / Math.max(cursor.total - 1, 1);
    }
    if (top !== undefined) {
      window.scrollTo(0, Math.max(top - view / 3, 0));
    }
  }

  // The top of `line` in the page, between the closest blocks before and
  // after it.
  function lineTop(line) {
    let before, after;
    for (const el of content.querySelectorAll('[data-line]')) {
      const l = Number(el.dataset.line);
      if (l <= line && (!before || l >= before.l)) {
        before = { el, l };
      } else if (l > line && (!after || l < after.l)) {
        after = { el, l };
      }
    }
    if (!before) {
      return 0;
    }
    const y = (el) => el.getBoundingClientRect().top + window.scrollY;
    const from = y(before.el);
    if (!after) {
      return from;
    }
    return from + (y(after.el) - from) * (line - before.l) / (after.l - before.l);
  }

  function reloadCSS() {
    const link = document.getElementById('custom-css');
    if (link) {
      link.href = '../../custom.css?' + Date.now();
    }
  }

  window.addEventListener('resize', scroll);

  const events = new EventSource('events');
  events.addEventListener('render', (e) => render(JSON.parse(e.data)));
  events.addEventListener('cursor', (e) => {
    cursor = JSON.parse(e.data);
    scroll();
  });
  events.addEventListener('css', reloadCSS);
  events.addEventListener('close', () => {
    events.close();
    document.title = document.title + ' (closed)';
  });
})();
