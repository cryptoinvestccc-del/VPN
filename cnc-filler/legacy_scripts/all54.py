import re, openpyxl
from openpyxl.styles import PatternFill, Font, Alignment
D='/tmp/claude-0/-home-claude/77365e1c-a1c8-5008-b397-5c19b94b801a/scratchpad/xl/'
my=openpyxl.load_workbook('/root/.claude/uploads/77365e1c-a1c8-5008-b397-5c19b94b801a/42b1d7f2-_________Huareal_2.xlsx')['Пластины']
mh=[c.value for c in my[1]]
rows=[dict(zip(mh,r)) for r in my.iter_rows(min_row=2,values_only=True) if r[1]]
up=openpyxl.load_workbook(D+'Пластины_Huareal_загрузка_1С.xlsx')['Загрузка']
H=[c.value for c in up[1]]
dc={r[0]:list(r) for r in up.iter_rows(min_row=2,values_only=True)}
upfill={}
for r in up.iter_rows(min_row=2):
    upfill[r[0].value]=[c.fill.fgColor.rgb=='00FFF2A8' or c.fill.fgColor.rgb=='FFFFF2A8' for c in r]
GR={'HR1135':('P M','P,M (Сталь, Нержавеющая сталь)','PVD','Твердосплавная'),
    'HR7125':('M','M (Нержавеющая сталь)','PVD','Твердосплавная'),
    'HRC10':('P','P (Сталь)','Без покрытия (кермет)','Металлокерамическая')}
SH={'C':'C (ромб 80°)','D':'D (ромб 55°)','S':'S (квадрат 90°)'}
VID={'XF':'Чистовая','PU':'Чистовая','FF':'Чистовая','TF':'Чистовая','HQ':'Чистовая','TM':'Получистовая'}
Y=PatternFill('solid',fgColor='FFF2A8')
wb=openpyxl.Workbook(); o=wb.active; o.title='Пластины'; o.append(H)
notes=[]
for r in rows:
    a=r['Артикул']; code=str(r['Код товара']).zfill(8)
    if a in dc:
        o.append(dc[a]); n=o.max_row
        for j,f in enumerate(upfill[a]):
            if f: o.cell(n,j+1).fill=Y
        o.cell(n,10).number_format='@'
        continue
    m=re.match(r'^([CDS])CGT|^([CDS])CMT',a)
    iso=a[:4]; sh=a[0]; gr=r['Марка сплава']; cb=r['Стружколом']
    mark=re.match(r'^[A-Z]{4}\d{2}(?:\d\d|T3)\d\d',a).group()
    th=re.match(r'^[A-Z]{4}\d{2}(\d\d|T3)',a).group(1)
    v=[None]*len(H)
    def s(k,val): v[H.index(k)]=val
    s('Артикул',a); s('Бренд','Huareal'); s('Диаметр вписанной окружности, мм',str(r['IC, мм'])); s('Обозначение СМП по ISO',iso)
    s('Задний угол пластины','C (7°)'); s('S, мм',r['Толщина S, мм']); s('_Код товара',code); s('r, мм',r['Радиус RE, мм'])
    s('Маркировка',mark); s('ØiC, мм',r['IC, мм']); s('Сплав',gr); s('Диаметр крепежного отверстия пластины d, мм',str(r['Отверстие DI, мм']))
    s('Форма пластины',SH[sh]); s('Размер пластины',int(a[4:6])); s('L, мм',r['Длина кромки LE, мм']); s('Толщина пластины',2 if th=='02' else 'T3')
    s('d, мм',r['Отверстие DI, мм']); s('Радиус при вершине, мм',str(r['Радиус RE, мм']))
    note=[]
    if cb in VID and r['Статус']!='нет в каталоге': s('Вид обработки',VID[cb])
    else: note.append(f'стружколома {cb} для этой пластины в каталоге нет — вид обработки не заполнен')
    if cb=='XF': note.append('XF в каталоге — сверхчистовая, поставил «Чистовая»')
    if gr in GR:
        g=GR[gr]; v[8]=g[0]; v[12]=g[1]; s('Покрытие',g[2]); s('Тип пластины',g[3])
        if gr=='HRC10': note.append('HRC10 — кермет без покрытия (каталог A14): «Керметная», «Без покрытия» — проверьте, есть ли такие значения в справочнике 1С')
    else: note.append(f'сплава {gr} в каталогах Huareal нет: материал, покрытие, тип пластины не заполнены')
    o.append(v); n=o.max_row; o.cell(n,10).number_format='@'
    for j,x in enumerate(v):
        if x not in (None,'') and j!=0: o.cell(n,j+1).fill=Y
    notes.append([code,a,'; '.join(note) or 'всё по каталогу'])
for c in o[1]: c.font=Font(bold=True); c.alignment=Alignment(wrap_text=True,vertical='center'); c.fill=PatternFill('solid',fgColor='DDE6F0')
o.freeze_panes='B2'
for col,w in zip('ABCDEFGHIJKLMNOPQRSTUVWXY',[24,10,9,8,9,8,6,8,8,10,6,13,30,12,7,11,9,9,15,7,6,7,6,12,8]): o.column_dimensions[col].width=w
# заметки: DCMT из прошлого файла + новые
old=openpyxl.load_workbook(D+'Пластины_Huareal_загрузка_1С.xlsx')['Что заполнено']
allnotes={x[1]:[x[0],x[1],x[3]] for x in old.iter_rows(min_row=2,values_only=True)}
for x in notes: allnotes[x[1]]=x
nn=wb.create_sheet('Примечания'); nn.append(['Код товара','Артикул','Примечание'])
for r in rows:
    if r['Артикул'] in allnotes: nn.append(allnotes[r['Артикул']])
nn.append([]); nn.append(['','Источник','2026-2027 Huareal Catalogue: A12–A14 (сплавы), A50 (DCMT), A130 (DCGT-FF); 2024-2025: A122 (CCGT-XF), A126 (DCGT-PU)'])
nn.column_dimensions['A'].width=11; nn.column_dimensions['B'].width=24; nn.column_dimensions['C'].width=110
for x in nn.iter_rows(min_row=2): x[0].number_format='@'
wb.save(D+'Пластины_Huareal_загрузка_1С.xlsx')
print(o.max_row-1)
for r in o.iter_rows(min_row=2,max_row=3,values_only=True): print(r)
for x in notes: print(x)
