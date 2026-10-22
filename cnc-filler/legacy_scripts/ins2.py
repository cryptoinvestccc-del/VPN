import re, openpyxl
from openpyxl.styles import Font, PatternFill, Alignment
from openpyxl.utils import get_column_letter
D='/tmp/claude-0/-home-claude/77365e1c-a1c8-5008-b397-5c19b94b801a/scratchpad/xl/'
src=openpyxl.load_workbook(D+'Пластины_по_артикулу.xlsx').active
A=[r[0] for r in src.iter_rows(min_row=2,values_only=True) if r[0]]
SHAPE={'C':'C (ромб 80°)','D':'D (ромб 55°)','S':'S (квадрат 90°)'}
ISO={('C','06'):(6.45,6.35),('D','07'):(7.75,6.35),('D','11'):(11.6,9.525),('S','09'):(9.525,9.525)}
THK={'02':2.38,'T3':3.97}; HOLE={6.35:2.8,9.525:4.4}
# Каталог Huareal 2024-2025: (LE, IC, S, DI) по размеру, серии: тип обработки, ★, ☆, державки, страница
CATDIM={('C','06'):(6.4,6.35,2.38,2.8),('D','07'):(7.8,6.35,2.38,2.8),('D','11'):(11.6,9.525,3.97,4.4)}
SER={
 'XF':('сверхчистовая (超精加工)',['HR1135','HR7125'],['HRC10'],'S-SCLC**-06','A122 (стр. 74 PDF)'),
 'PU':('чистовая (精加工)',['HR1135','HR7125'],['HRC10'],'SDJC**-07/11, SDNC**-07/11, S-SDUC**-07/11','A126 (стр. 76 PDF)'),
 'TF':('чистовая (Finishing)',['HR8115','HR8225','HR7225','HR9205'],['HR8125','HR7125','HR9215'],'S**-SDQCR/L, S**-SDUCR/L, C**-SDQCR/L, C**-SDUCR/L','каталог 2026-2027, A50 (стр. 41 PDF)'),
 'FF':('чистовая (Finishing)',['HR7125'],[],'SDJC**-07/11, SDNC**-07/11, S-SDUC**-07/11','каталог 2026-2027, A130 (стр. 81 PDF)'),
 'TM':('получистовая (半精加工)',['HR8115','HR8225','HR7225'],['HR8125','HR7125'],'S**-SDQCR/L (A88), S**-SDUCR/L (A89)','A44 (стр. 35 PDF)'),
 'HQ':('чистовая и получистовая (精半加工), кермет',['HRC10'],['HR115C'],'—','A55 (стр. 41 PDF)'),
}
# Какие пластины (без сплава) есть в каталоге
IN={'CCGT060202L-XF'}
IN|={f'DCGT{s}{r}{h}-PU' for s in ('0702','11T3') for r in ('01','02','04') for h in 'RL'}
IN|={f'DCGT{x}-FF' for x in ('070201','070202','070204','11T301','11T302','11T304')}
IN|={f'DCMT{x}-TF' for x in ('070202','070204','070208','11T302')}
IN|={'DCMT11T304-TF','DCMT11T308-TF','DCMT070204-TM','DCMT070208-TM','DCMT11T304-TM','DCMT11T308-TM','DCMT11T312-TM'}
IN|={f'DCMT{x}HQ' for x in ('070202','070204','070208','11T302','11T304','11T308')}
KY={'DCMT11T304-TF','DCMT11T308-TF'}; KYGR=['HR8115','HR8225','HR7225','HR7115','HR9205','HR9215']
ALLGR={'HR9205','HR9215','HR7240','HR9240','HR8105','HR8115','HR8125','HR8225','HR8135','HR6115','HR1135','HR9105','HR7115','HR7125','HR7225','HR5125','HR5225','HRK10','HRK20','HRC10','HR115C'}
WHY_NO={'XX':'стружколома FF в каталоге Huareal нет ни для одной пластины (для DCGT есть XF, PU, PJ, AK)',
        'TF07':'DCMT…-TF в каталоге есть только 11T304 и 11T308; размера 0702 с TF нет (для 0702 есть -TM и HQ)',
        'MP':'SCMT09T308 в каталоге есть только со стружколомом -TM (стр. 36 PDF), -MP нет'}
head=['Артикул','Статус','Форма','Задний угол','Класс точности','Тип','Длина кромки LE, мм','IC, мм','Толщина S, мм','Отверстие DI, мм','Радиус RE, мм','Исполнение','Стружколом','Марка сплава','Сплав по каталогу','Тип обработки','Основные сплавы ★','Дополнительные ☆','Державки','Страница каталога','Откуда размеры / примечание']
wb=openpyxl.Workbook(); ws=wb.active; ws.title='Пластины'; ws.append(head)
G=PatternFill('solid',fgColor='D9F2D0'); Yl=PatternFill('solid',fgColor='FFF2A8'); R=PatternFill('solid',fgColor='F8CBAD')
stat={}
for a in A:
    m=re.match(r'^([CDS])(C)([GM])(T)(\d{2})(\d\d|T3)(\d{2})([LR]?)(HQ)?(?:-([A-Z]{2}))?-(HR\d{3}C|H[RS][A-Z]?\d+)$',a)
    sh,cl,tl,ty,sz,th,r,hand,hq,cb,gr=m.groups(); cb=cb or hq
    base=f'{sh}{cl}{tl}{ty}{sz}{th}{r}{hand}'+('HQ' if hq else '-'+cb)
    ic=ISO[(sh,sz)][1]; rr=int(r)/10
    row=[a,'',SHAPE[sh],'7°',tl,'T',None,None,None,None,rr,{'L':'L (левое)','R':'R (правое)'}.get(hand,'N (нейтральное)'),cb,gr]
    if base in IN:
        le,ic,s,di=CATDIM[(sh,sz)]; typ,st,op,hold,pg=SER[cb]; st=list(st)
        row[6:10]=[le,ic,s,di]
        if gr in st: gs='★ основной'; status='найдено'
        elif gr in op: gs='☆ дополнительный'; status='найдено'
        elif base in KY and gr in KYGR:
            gs='★ (буклет Kylin 2025, стр. 22)'; status='найдено'; pg+=' + буклет Kylin 2025, стр. 22 (стр. 14 PDF)'
        elif gr in ALLGR: gs=f'для этой пластины {gr} в каталоге не указан'; status='пластина есть, сплав не указан'
        else: gs=f'сплава {gr} в каталоге нет'; status='пластина есть, сплава нет'
        row+= [gs,typ,', '.join(st),', '.join(op),hold,pg,'размеры из каталога Huareal']
    else:
        l,ic=ISO[(sh,sz)]; row[6:10]=[l,ic,THK[th],HOLE[ic]]
        why=WHY_NO['FF'] if cb=='FF' else WHY_NO['MP'] if cb=='MP' else WHY_NO['TF07'] if cb=='TF' else 'нет в каталоге'
        gs='' if gr in ALLGR else f'сплава {gr} в каталоге нет'
        status='нет в каталоге'; row+=[gs,'','','','','—','размеры по ISO 1832 из артикула; '+why]
    row[1]=status; stat[status]=stat.get(status,0)+1
    ws.append(row); f=G if status=='найдено' else Yl if status.startswith('пластина') else R
    for c in ws[ws.max_row]: c.fill=f
for c in ws[1]: c.font=Font(bold=True); c.fill=PatternFill('solid',fgColor='DDE6F0'); c.alignment=Alignment(wrap_text=True,horizontal='center',vertical='center')
ws.row_dimensions[1].height=45
for i,w in enumerate([24,22,14,8,8,5,9,8,8,9,8,14,9,10,24,26,24,18,34,18,60],1): ws.column_dimensions[get_column_letter(i)].width=w
for row in ws.iter_rows(min_row=2):
    for c in row: c.alignment=Alignment(vertical='top',wrap_text=c.column in (2,15,16,19,21))
ws.freeze_panes='B2'; ws.auto_filter.ref=ws.dimensions
n=wb.create_sheet('Легенда')
for rw in [['Цвет','Значение'],['зелёный','пластина и сплав есть в каталоге Huareal 2024-2025: всё заполнено из каталога'],['жёлтый','пластина есть в каталоге, но такого сплава для неё в каталоге нет: размеры из каталога, сплав проверить у поставщика'],['красный','такой пластины в каталоге нет: размеры расшифрованы по ISO 1832 из артикула, остальное не заполнено'],['',''],['Сплавы, которых нет ни в одном каталоге (2024-2025, 2026-2027) и в буклете','HS1135, HS6115, HS7125, HS8125 (в каталоге есть HR1135, HR6115, HR7125, HR8125 — возможно, другая серия или опечатка в артикуле, уточнить у поставщика)'],['HR9205 / HR9215','новые сплавы 2025 г. серии Kylin для жаропрочных и труднообрабатываемых сплавов. HR9205 — высокая износостойкость; HR9215 — универсальный, повышенная прочность (покрытие AlCrN/AlTiN/TiSiN). Источник: буклет Kylin 2025, стр. 3. В каталоге 2026-2027 для DCMT…-TF: HR9205 ★, HR9215 ☆; в буклете Kylin обе ★ (для 11T304/308)'],['LE по ISO и по каталогу','ISO для D07 даёт 7.75 мм, каталог Huareal пишет 7.8; для C06 — 6.45 и 6.4. Для найденных пластин взято значение каталога'],['Источник','2026-2027 Huareal Catalogue (A50 — DCMT-TF, A130 — DCGT-FF); 2024-2025 Huareal Catalogue CN, стр. A44, A55, A122, A126; буклет «Kylin 2025» (HR9205/HR9215/HR7240/HR9240), стр. 22']]: n.append(rw)
n.column_dimensions['A'].width=28; n.column_dimensions['B'].width=110
for c in n[1]: c.font=Font(bold=True)
n['A2'].fill=G; n['A3'].fill=Yl; n['A4'].fill=R
wb.save(D+'Пластины_Huareal.xlsx'); print(stat)
for r in ws.iter_rows(min_row=2,values_only=True):
    if r[1]!='найдено': print(r[0],'|',r[1],'|',r[14])
