#!/usr/bin/env python3
"""Search catalog index for article codes.
Usage: find.py CODE [CODE...]
Levels: EXACT (full code), FUZZY (OCR-confusable chars), SPLIT (series + size in same page),
SERIES (only the letter series, e.g. SCLCR) -> candidate pages to open visually."""
import sys, os, re, glob
IDX = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'idx')
CONF = str.maketrans({'O': '0', 'Q': '0', 'D': '0', 'I': '1', 'L': '1', '|': '1', 'S': '5', 'B': '8', 'Z': '2', 'G': '6'})

def norm(s):
    return re.sub(r'[^A-Z0-9]', '', s.upper())

def fuzzy(s):
    return norm(s).translate(CONF)

pages = []
for f in sorted(glob.glob(os.path.join(IDX, '*', 'p*.txt'))):
    cat = os.path.basename(os.path.dirname(f))
    pg = int(re.sub(r'\D', '', os.path.basename(f)) or 0)
    t0 = open(f, errors='ignore').read()
    t = t0 + '\n' + re.sub(r'([RL])\s*/\s*([RL])', r'\1', t0) + '\n' + re.sub(r'([RL])\s*/\s*([RL])', r'\2', t0)
    lines = t.splitlines()
    pages.append((cat, pg, lines, norm(t), fuzzy(t)))

def ctx(lines, keys, n=2):
    out = []
    for x in lines:
        nx, fx = norm(x), fuzzy(x)
        if any(k in nx or k.translate(CONF) in fx for k in keys):
            s = re.sub(r'\s+', ' ', x.strip())[:180]
            if s not in out:
                out.append(s)
        if len(out) >= n:
            break
    return ' || '.join(out)

for q in sys.argv[1:]:
    nq, fq = norm(q), fuzzy(q)
    print(f'=== {q}')
    seen = set()
    def show(tag, hits, keys):
        for c, p, l in hits:
            if (c, p) in seen:
                continue
            seen.add((c, p))
            print(f'{tag:7} {c:10} p{p:<4} {ctx(l, keys)}')
    show('EXACT', [(c, p, l) for c, p, l, n, f in pages if nq in n], [nq])
    show('FUZZY', [(c, p, l) for c, p, l, n, f in pages if fq in f], [nq])
    m = re.match(r'^([A-Z]+?)([0-9].*)$', nq)
    if m and len(m.group(1)) >= 2 and len(m.group(2)) >= 3:
        a, b = m.group(1), m.group(2)
        show('SPLIT', [(c, p, l) for c, p, l, n, f in pages if a in n and (b in n or b.translate(CONF) in f)], [b])
    runs = sorted([r for tok in re.split(r'[^A-Z0-9]+', q.upper()) for r in re.findall(r'[A-Z]{4,}', tok)]
                  or re.findall(r'[A-Z]{4,}', nq), key=len, reverse=True)
    if runs:
        s = runs[0]
        byc = {}
        for c, p, l, n, f in pages:
            if s in n and (c, p) not in seen:
                byc.setdefault(c, []).append(p)
        for c, ps in byc.items():
            print(f'SERIES  {c:10} {s}: pages {ps[:25]}')
    if not seen and not runs:
        print('NOT FOUND')
