#!/usr/bin/env python3
"""Gera web/static/data/relevo-sp.js: o relevo do Estado de Sao Paulo.

O arquivo define window.SP_RELEVO com as regioes {altitude >= t}, para
t = 200, 400, ... metros, recortadas pelo contorno do Estado. O mapa usa o
mesmo conjunto de aneis para duas camadas:

    preenchidos  ->  hipsometria (faixas altimetricas)
    tracados     ->  curvas de nivel

Fonte: SRTM30 versao 2.1 (NASA/USGS), 30 segundos de arco (~900 m),
altitudes ortometricas referidas ao geoide EGM96. O Estado inteiro cabe na
folha w060s10 (60 W a 20 W, 10 S a 60 S). Nenhuma altitude e inventada nem
suavizada: as curvas sao a interpolacao linear da grade original
(marching squares, pelo contourpy). Cada celula e a media de cerca de 0,8 km2,
entao os picos isolados saem abaixo da altitude do cume (a Pedra da Mina, com
2798 m, aparece com cerca de 2600 m).

Uso, da raiz do repositorio:

    pip install numpy contourpy shapely
    python3 tools/build-relevo.py

A folha (8 MB compactada) e baixada uma vez para tools/.cache/. Primeiro do
servidor da USGS; se ele nao responder, de um espelho no GitHub com os
mesmos arquivos.
"""

import io
import json
import sys
import urllib.request
import zipfile
from pathlib import Path

import contourpy
import numpy
import shapely
from shapely.geometry import MultiPolygon, Polygon, shape

RAIZ = Path(__file__).resolve().parent.parent
SAIDA = RAIZ / "web" / "static" / "data" / "relevo-sp.js"
MALHA = RAIZ / "web" / "static" / "data" / "sp-geo.js"
CACHE = Path(__file__).resolve().parent / ".cache" / "srtm30"

FOLHA = "w060s10"
FONTES = (
    "https://dds.cr.usgs.gov/srtm/version2_1/SRTM30/{f}/{f}.{ext}.zip",
    # Espelho da distribuicao da USGS (repositorio K-Francis-H/srtm30).
    "https://raw.githubusercontent.com/K-Francis-H/srtm30/master/data/{f}.{ext}.zip",
)

# Intervalo entre curvas, em metros: o das cartas topograficas do IBGE nesta
# escala. Resolve a Mantiqueira e o degrau da Serra do Mar sem encher o
# Planalto Ocidental de linhas.
INTERVALO = 200

# Margem em graus em volta do Estado. As regioes sao calculadas numa area um
# pouco maior e so depois recortadas pelo contorno, para a borda das faixas
# coincidir com a divisa desenhada no mapa, e nao com a grade de 900 m.
MARGEM = 0.1

# Douglas-Peucker sobre cada nivel, em graus. 0,0015 grau e cerca de 160 m,
# um quinto da celula: tira os pontos colineares da interpolacao sem mexer em
# nada que a fonte resolva. preserve_topology impede que um anel se cruze.
TOLERANCIA = 0.0015

# Aneis menores que isto saem do arquivo, em graus quadrados (0,0004 grau
# quadrado e cerca de 5 km2, umas seis celulas). Sao manchas de uma ou duas
# celulas que, no zoom util do mapa, mal chegam a um pixel. Vale para ilhas e
# para furos. Excecao: o maior pedaco de cada nivel sempre fica, para que o
# topo da Serra Fina, na divisa com Minas (acima de 2400 m, cerca de 2 km2),
# continue no mapa.
AREA_MIN = 0.0004

# Casas decimais das coordenadas: 4 casas sao cerca de 11 m.
PRECISAO = 4


def baixar(extensao):
    """Conteudo do arquivo .DEM ou .HDR da folha, do cache ou da rede."""
    destino = CACHE / ("%s.%s" % (FOLHA, extensao))
    if destino.exists():
        return destino.read_bytes()

    ultimo_erro = None
    for modelo in FONTES:
        url = modelo.format(f=FOLHA, ext=extensao)
        try:
            pedido = urllib.request.Request(url, headers={"User-Agent": "ClimaSP/1.0"})
            with urllib.request.urlopen(pedido, timeout=120) as resposta:
                compactado = resposta.read()
        except OSError as erro:
            print("  %s: %s" % (url, erro))
            ultimo_erro = erro
            continue
        with zipfile.ZipFile(io.BytesIO(compactado)) as arquivo:
            nome = next(n for n in arquivo.namelist() if n.lower().endswith("." + extensao))
            dados = arquivo.read(nome)
        CACHE.mkdir(parents=True, exist_ok=True)
        destino.write_bytes(dados)
        print("  %s (%d kB)" % (url, len(compactado) // 1024))
        return dados
    raise SystemExit("nao foi possivel baixar %s.%s: %s" % (FOLHA, extensao, ultimo_erro))


def ler_folha():
    """Devolve (altitudes int16, longitude do centro da 1a coluna, latitude
    do centro da 1a linha, passo em graus)."""
    cabecalho = {}
    for linha in baixar("hdr").decode("ascii").splitlines():
        partes = linha.split()
        if len(partes) == 2:
            cabecalho[partes[0].upper()] = partes[1]

    linhas, colunas = int(cabecalho["NROWS"]), int(cabecalho["NCOLS"])
    ordem = ">" if cabecalho.get("BYTEORDER", "M") == "M" else "<"
    altitudes = numpy.frombuffer(baixar("dem"), dtype=ordem + "i2").reshape(linhas, colunas)
    # ULXMAP e ULYMAP sao o CENTRO do pixel do canto noroeste.
    return (
        altitudes,
        float(cabecalho["ULXMAP"]),
        float(cabecalho["ULYMAP"]),
        float(cabecalho["XDIM"]),
    )


def ler_estado():
    """Contorno do Estado, o mesmo que o mapa desenha (sp-geo.js)."""
    texto = MALHA.read_text(encoding="utf-8")
    inicio = texto.index("window.SP_GEOJSON = ") + len("window.SP_GEOJSON = ")
    dados = json.loads(texto[inicio:].strip().rstrip(";"))
    # make_valid em cada parte antes de unir: a malha simplificada tem
    # pequenas auto-intersecoes no litoral sul, que a uniao recusaria.
    partes = [shapely.make_valid(shape(f["geometry"])) for f in dados["features"]]
    return shapely.union_all(partes)


def recortar_grade(altitudes, x0, y0, passo, caixa):
    """Recorta a folha na caixa (oeste, sul, leste, norte) e devolve a grade
    com os vetores de longitude e latitude dos centros das celulas."""
    oeste, sul, leste, norte = caixa
    c0 = int(numpy.floor((oeste - x0) / passo))
    c1 = int(numpy.ceil((leste - x0) / passo)) + 1
    l0 = int(numpy.floor((y0 - norte) / passo))
    l1 = int(numpy.ceil((y0 - sul) / passo)) + 1
    grade = altitudes[l0:l1, c0:c1].astype(numpy.float64)
    lons = x0 + numpy.arange(c0, c1) * passo
    lats = y0 - numpy.arange(l0, l1) * passo
    if (grade == -9999).any():
        raise SystemExit("a folha tem celulas sem dado dentro da area do Estado")
    return grade, lons, lats


def regiao_acima(gerador, nivel):
    """Regiao {altitude >= nivel} como MultiPolygon, com os furos."""
    poligonos, codigos = gerador.filled(nivel, 1e9)
    saida = []
    for pontos, offsets in zip(poligonos, codigos):
        # Com FillType.OuterOffset, cada item e um poligono: o primeiro anel e
        # o externo e os seguintes sao furos.
        aneis = [pontos[offsets[k]:offsets[k + 1]] for k in range(len(offsets) - 1)]
        saida.append(Polygon(aneis[0], aneis[1:]))
    return shapely.make_valid(MultiPolygon(saida)) if saida else None


def poligonos_de(geometria):
    """Os Polygons de uma geometria qualquer (a intersecao pode devolver
    colecoes com linhas e pontos soltos, que nao tem area)."""
    if geometria is None or geometria.is_empty:
        return []
    if geometria.geom_type == "Polygon":
        return [geometria]
    if hasattr(geometria, "geoms"):
        return [p for parte in geometria.geoms for p in poligonos_de(parte)]
    return []


def limpar(geometria):
    """Tira ilhas e furos pequenos e devolve a lista de Polygons. O maior
    pedaco fica sempre, mesmo abaixo de AREA_MIN."""
    poligonos = poligonos_de(geometria)
    if not poligonos:
        return []
    maior = max(poligonos, key=lambda p: p.area)
    saida = []
    for parte in poligonos:
        if parte.area < AREA_MIN and parte is not maior:
            continue
        furos = [anel for anel in parte.interiors if Polygon(anel).area >= AREA_MIN]
        saida.append(Polygon(parte.exterior, furos))
    return saida


def para_aneis(poligonos):
    """Aneis em [lat, lon], arredondados: o que L.polygon consome. Externos e
    furos vao juntos na mesma lista; o mapa pinta com fillRule evenodd, que
    faz de cada anel interno um furo sem precisar saber qual e qual."""
    aneis = []
    for poligono in poligonos:
        for anel in [poligono.exterior, *poligono.interiors]:
            pontos = [[round(y, PRECISAO), round(x, PRECISAO)] for x, y in anel.coords]
            # O arredondamento pode repetir pontos vizinhos.
            unicos = [p for k, p in enumerate(pontos) if k == 0 or p != pontos[k - 1]]
            if len(unicos) >= 4:
                aneis.append(unicos)
    return aneis


def main():
    print("Folha %s do SRTM30:" % FOLHA)
    altitudes, x0, y0, passo = ler_folha()

    estado = ler_estado()
    oeste, sul, leste, norte = estado.bounds
    caixa = (oeste - MARGEM, sul - MARGEM, leste + MARGEM, norte + MARGEM)
    grade, lons, lats = recortar_grade(altitudes, x0, y0, passo, caixa)
    print("Grade: %d x %d celulas de %.5f grau" % (grade.shape[1], grade.shape[0], passo))

    # Mascara da grade pelo Estado, so para informar a altitude maxima.
    xx, yy = numpy.meshgrid(lons, lats)
    dentro = shapely.contains_xy(estado, xx, yy)
    maxima = int(grade[dentro].max())
    print("Altitude maxima dentro do Estado: %d m" % maxima)

    gerador = contourpy.contour_generator(
        lons, lats, grade,
        name="serial",
        fill_type=contourpy.FillType.OuterOffset,
        corner_mask=False,
    )

    niveis = []
    total = 0
    # Cada nivel e simplificado sozinho, o que poderia fazer uma faixa vazar
    # alguns metros para fora da faixa de baixo e mostrar a cor errada na
    # borda. Intersectar com o nivel anterior, ja simplificado, garante que
    # {altitude >= 400} esteja sempre dentro de {altitude >= 200}.
    abaixo = estado
    for altitude in range(INTERVALO, maxima + 1, INTERVALO):
        regiao = regiao_acima(gerador, float(altitude))
        if regiao is None:
            continue
        recortada = shapely.intersection(regiao, estado)
        simplificada = shapely.simplify(recortada, TOLERANCIA, preserve_topology=True)
        encaixada = shapely.make_valid(shapely.intersection(simplificada, abaixo))
        partes = limpar(encaixada)
        if not partes:
            continue
        abaixo = shapely.union_all(partes)
        aneis = para_aneis(partes)
        if not aneis:
            continue
        pontos = sum(len(a) for a in aneis)
        total += pontos
        niveis.append({"altitude": altitude, "aneis": aneis})
        print("  %4d m: %4d aneis, %6d pontos, %7.0f km2" % (
            altitude, len(aneis), pontos, recortada.area * 111.32 * 111.32 * 0.92))

    resultado = {
        "intervalo": INTERVALO,
        "fonte": {
            "nome": "SRTM30 (NASA/USGS)",
            "url": "https://www.usgs.gov/centers/eros/science/usgs-eros-archive-digital-elevation-shuttle-radar-topography-mission-srtm",
        },
        "niveis": niveis,
    }
    corpo = json.dumps(resultado, ensure_ascii=False, separators=(",", ":"))
    cabecalho = (
        "/* Relevo do Estado de Sao Paulo. Nao editar a mao.\n"
        "   Gerado por tools/build-relevo.py.\n"
        "   Fonte: SRTM30 v2.1 (NASA/USGS), folha %s, 30 segundos de arco\n"
        "   (~900 m), altitudes referidas ao geoide EGM96.\n"
        "   Regioes {altitude >= t}, t = %d, %d, ... m, recortadas pelo\n"
        "   contorno de sp-geo.js. Coordenadas [lat, lon].\n"
        "   Simplificacao: Douglas-Peucker, tolerancia %s grau. */\n\n"
        "window.SP_RELEVO = " % (FOLHA, INTERVALO, INTERVALO * 2, TOLERANCIA)
    )
    SAIDA.write_text(cabecalho + corpo + ";\n", encoding="utf-8")
    print("%s: %d niveis, %d pontos, %.0f kB" % (
        SAIDA.relative_to(RAIZ), len(niveis), total, SAIDA.stat().st_size / 1024))
    return 0


if __name__ == "__main__":
    sys.exit(main())
