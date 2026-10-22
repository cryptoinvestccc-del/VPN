import re, openpyxl
from openpyxl.styles import Font, PatternFill, Alignment
A='''CCGT060202L-XF-HS1135 DCGT070201-FF-HR7125 DCGT070202-FF-HR7125 DCGT070202L-PU-HR1135 DCGT070202R-PU-HR1135 DCGT070204-FF-HR7125 DCGT070204L-PU-HR1135 DCGT070204R-PU-HR1135 DCGT11T301-FF-HR7125 DCGT11T301R-PU-HR1135 DCGT11T302-FF-HR7125 DCGT11T302L-PU-HR1135 DCGT11T302L-PU-HRC10 DCGT11T302R-PU-HR1135 DCGT11T302R-PU-HRC10 DCGT11T304-FF-HR7125 DCGT11T304L-PU-HR1135 DCGT11T304L-PU-HRC10 DCGT11T304R-PU-HR1135 DCGT11T304R-PU-HRC10 DCMT070202HQ-HR115C DCMT070202-TF-HR7225 DCMT070202-TF-HR8225 DCMT070202-TF-HR9205 DCMT070202-TF-HR9215 DCMT070204HQ-HR115C DCMT070204-TF-HR7115 DCMT070204-TF-HR7225 DCMT070204-TF-HR8225 DCMT070204-TF-HR9205 DCMT070204-TF-HR9215 DCMT070208HQ-HR115C DCMT070208-TF-HR7115 DCMT070208-TF-HR7225 DCMT070208-TF-HR8225 DCMT070208-TF-HR9205 DCMT070208-TF-HR9215 DCMT11T304HQ-HR115C DCMT11T304-TF-HR7115 DCMT11T304-TF-HR9205 DCMT11T304-TF-HR9215 DCMT11T304-TM-HS6115 DCMT11T304-TM-HS7125 DCMT11T304-TM-HS8125 DCMT11T308HQ-HR115C DCMT11T308-TF-HR7115 DCMT11T308-TF-HR9205 DCMT11T308-TF-HR9215 DCMT11T312-TM-HR7225 DCMT11T312-TM-HR8225 DCMT11T312-TM-HS6115 DCMT11T312-TM-HS7125 DCMT11T312-TM-HS8125 SCMT09T308-MP-HS7125'''.split()
SHAPE={'C':'C (ромб 80°)','D':'D (ромб 55°)','S':'S (квадрат 90°)'}
CLR={'C':'C (7°)'}
TOL={'G':'G','M':'M'}
TYP={'T':'T (с отверстием, односторонний стружколом, зенковка 40–60°)'}
# ISO 1832: (shape, size code) -> (edge length l, inscribed circle IC)
SIZE={('C','06'):(6.45,6.35),('D','07'):(7.75,6.35),('D','11'):(11.6,9.525),('S','09'):(9.525,9.525)}
THK={'02':2.38,'T3':3.97}
HOLE={6.35:2.8,9.525:4.4}
out=openpyxl.Workbook(); ws=out.active; ws.title='Пластины'
head=['Артикул','Форма пластины','Задний угол','Класс точности','Тип пластины','Размер пластины (код)','Длина режущей кромки l, мм','Вписанная окружность d (IC), мм','Толщина s, мм','Отверстие d1, мм','Радиус при вершине r, мм','Исполнение','Стружколом','Марка сплава']
ws.append(head)
for a in A:
    m=re.match(r'^([CDS])([C])([GM])([T])(\d{2})(\d\d|T3)(\d{2})([LR]?)(HQ)?-?([A-Z]{2})?-?([A-Z0-9]+)$',a)
    sh,cl,tl,ty,sz,th,r,hand,hq,cb,gr=m.groups()
    if hq: cb='HQ'
    if cb==gr: cb=None
    l,ic=SIZE[(sh,sz)]
    ws.append([a,SHAPE[sh],CLR[cl],tl,TYP[ty],sz,l,ic,THK[th],HOLE[ic],int(r)/10,{'L':'L (левое)','R':'R (правое)'}.get(hand,'N (нейтральное)'),cb,gr])
for c in ws[1]: c.font=Font(bold=True); c.fill=PatternFill('solid',fgColor='DDE6F0'); c.alignment=Alignment(wrap_text=True,horizontal='center',vertical='center')
ws.row_dimensions[1].height=45
for row in ws.iter_rows(min_row=2):
    for c in row: c.alignment=Alignment(horizontal='left' if c.column==1 else 'center')
for i,w in enumerate([26,16,11,9,22,10,12,13,10,10,11,15,11,12],1): ws.column_dimensions[openpyxl.utils.get_column_letter(i)].width=w
ws.freeze_panes='B2'
n=out.create_sheet('Как расшифровано')
for row in [['Позиция кода','Значение','Источник'],['1-я буква (C/D/S)','форма: C ромб 80°, D ромб 55°, S квадрат','ISO 1832'],['2-я буква C','задний угол 7°','ISO 1832'],['3-я буква G/M','класс точности','ISO 1832'],['4-я буква T','отверстие, односторонний стружколом','ISO 1832'],['цифры 06/07/11/09','длина кромки; IC 6.35 (06 C, 07 D) или 9.525 (11 D, 09 S)','ISO 1832'],['02 / T3','толщина 2.38 / 3.97 мм','ISO 1832'],['01/02/04/08/12','радиус 0.1/0.2/0.4/0.8/1.2 мм','ISO 1832'],['L / R / нет','исполнение левое / правое / нейтральное','ISO 1832'],['отверстие d1','2.8 мм при IC 6.35; 4.4 мм при IC 9.525 — стандартный крепёжный винт для пластин типа T','стандарт для T-пластин'],['XF, FF, PU, TF, TM, MP, HQ','стружколом производителя — переписан как есть','артикул'],['HR7125, HS1135, HRC10, HR115C…','марка сплава производителя — переписана как есть','артикул']]: n.append(row)
for c in n[1]: c.font=Font(bold=True)
for i,w in enumerate([28,70,22],1): n.column_dimensions[openpyxl.utils.get_column_letter(i)].width=w
out.save('Пластины_по_артикулу.xlsx'); print(ws.max_row-1)
