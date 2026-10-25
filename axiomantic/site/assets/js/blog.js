'use strict';
/* The blog: rubric filters, reactions and comments. Reactions and comments
   live on the server (data-blog on #feed); a comment shows after the team
   approves it in the panel. Comments are anonymous and drawn as text only. */
document.addEventListener('DOMContentLoaded', () => {
  const feed = document.getElementById('feed');
  if (!feed) return;
  const api = feed.dataset.blog || '';
  let MSG = {};
  try { MSG = JSON.parse(feed.dataset.blogMsg || '{}'); } catch { MSG = {}; }
  const posts = Array.from(feed.querySelectorAll('article[data-post]'));

  /* filters */
  const filterBtns = document.querySelectorAll('.filter-btn');
  filterBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      filterBtns.forEach(b => {
        b.classList.remove('bg-dark', 'text-white');
        b.classList.add('bg-surface-muted', 'text-text-body');
        b.setAttribute('aria-pressed', 'false');
      });
      btn.classList.remove('bg-surface-muted', 'text-text-body');
      btn.classList.add('bg-dark', 'text-white');
      btn.setAttribute('aria-pressed', 'true');
      const filter = btn.dataset.filter;
      posts.forEach(post => post.classList.toggle('hidden', !(filter === 'all' || post.dataset.category === filter)));
    });
  });

  /* this browser's own reactions, so a second click takes one back */
  const mineKey = 'axiomantic_reactions';
  let mine = {};
  try { mine = JSON.parse(localStorage.getItem(mineKey) || '{}'); } catch { mine = {}; }
  const saveMine = () => { try { localStorage.setItem(mineKey, JSON.stringify(mine)); } catch { /* storage off */ } };

  const draw = (post, stats) => {
    const s = stats || {};
    [['like', '.like-btn', 'liked'], ['dislike', '.dislike-btn', 'disliked']].forEach(([kind, sel, cls]) => {
      const btn = post.querySelector(sel);
      const n = Number(btn.dataset.base || 0) + Number(s[kind + 's'] || 0);
      btn.querySelector('span').textContent = String(Math.max(0, n));
      const on = mine[post.dataset.post] === kind;
      btn.classList.toggle(cls, on);
      btn.setAttribute('aria-pressed', String(on));
      const icon = btn.querySelector('i');
      icon.classList.toggle('fa-solid', on);
      icon.classList.toggle('fa-regular', !on);
    });
    const count = post.querySelector('.comment-count');
    if (count) count.textContent = s.comments ? '(' + s.comments + ')' : '';
  };

  const comment = (text) => {
    const div = document.createElement('div');
    div.className = 'bg-surface p-3 rounded-xl text-sm text-text-body border border-slate-100';
    const author = document.createElement('span');
    author.className = 'font-semibold text-text-heading';
    author.textContent = (MSG.anon || 'Аноним') + ': ';
    div.append(author, document.createTextNode(text));
    return div;
  };

  if (!api) {
    // a preview or a site without the server: buttons stay, nothing is sent
    posts.forEach(post => draw(post, {}));
  } else {
    fetch(api + '/stats', { credentials: 'same-origin' }).then(r => (r.ok ? r.json() : {})).then(all => {
      posts.forEach(post => draw(post, (all.posts || {})[post.dataset.post]));
    }).catch(() => posts.forEach(post => draw(post, {})));
  }

  posts.forEach(post => {
    const id = post.dataset.post;
    const react = kind => {
      if (!api) return;
      const prev = mine[id] || '';
      const next = prev === kind ? '' : kind;
      fetch(api + '/react', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin',
        body: JSON.stringify({ post: id, kind: next, prev })
      }).then(r => (r.ok ? r.json() : null)).then(stats => {
        if (!stats) return;
        if (next) mine[id] = next; else delete mine[id];
        saveMine();
        draw(post, stats);
      }).catch(() => {});
    };
    post.querySelector('.like-btn').addEventListener('click', () => react('like'));
    post.querySelector('.dislike-btn').addEventListener('click', () => react('dislike'));

    const toggle = post.querySelector('.comment-toggle');
    const section = post.querySelector('.comments-section');
    const list = post.querySelector('.comments-list');
    const status = post.querySelector('.comment-status');
    let loaded = false;
    toggle.addEventListener('click', () => {
      section.classList.toggle('hidden');
      const open = !section.classList.contains('hidden');
      toggle.setAttribute('aria-expanded', String(open));
      if (!open) return;
      section.querySelector('input[name="text"]').focus();
      if (loaded) return;
      loaded = true;
      if (!api) { status.textContent = MSG.off || ''; return; }
      fetch(api + '/comments?post=' + encodeURIComponent(id), { credentials: 'same-origin' })
        .then(r => (r.ok ? r.json() : { items: [] }))
        .then(d => {
          list.replaceChildren(...(d.items || []).map(c => comment(String(c.text || ''))));
          if (!(d.items || []).length) status.textContent = MSG.empty || '';
        }).catch(() => { status.textContent = MSG.failed || ''; });
    });

    post.querySelector('.comment-form').addEventListener('submit', event => {
      event.preventDefault();
      const form = event.currentTarget;
      const input = form.querySelector('input[name="text"]');
      const text = input.value.trim();
      if (!text) return;
      if (!api) { status.textContent = MSG.off || ''; return; }
      const btn = form.querySelector('[type="submit"]');
      btn.disabled = true;
      fetch(api + '/comments', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'same-origin',
        body: JSON.stringify({ post: id, text, website: form.querySelector('[name="website"]').value })
      }).then(r => {
        if (!r.ok) throw new Error(String(r.status));
        input.value = '';
        status.textContent = MSG.pending || '';
      }).catch(() => { status.textContent = MSG.failed || ''; })
        .finally(() => { btn.disabled = false; });
    });
  });
});
