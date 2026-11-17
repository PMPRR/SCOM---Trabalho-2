/* ==========================================================================
   station-form.js — mapa do formulario de estacao (station_form.html)
   --------------------------------------------------------------------------
   Um atalho para preencher latitude e longitude: clicar no mapa escreve os
   dois campos, e digitar nos campos move o marcador. Abaixo dos campos
   aparece o municipio do ponto, para conferir. Sem este arquivo, ou sem o
   Leaflet, o formulario funciona igual, so digitando.
   ========================================================================== */

(function () {
  "use strict";

  const el = document.getElementById("form-mapa");
  const campoLat = document.getElementById("lat");
  const campoLon = document.getElementById("lon");
  const aviso = document.getElementById("municipio");
  if (!el || !campoLat || !campoLon || typeof L === "undefined" || !window.SP_GEOJSON) return;

  function token(nome, padrao) {
    const valor = getComputedStyle(document.documentElement).getPropertyValue(nome).trim();
    return valor || padrao;
  }

  /* O mapa e so para o ponteiro: quem usa teclado ou leitor de tela preenche
     os campos, que sao a forma completa. Sem foco nem teclado, ele nao
     prende a tabulacao. */
  const mapa = L.map(el, {
    zoomSnap: 0.25,
    minZoom: 5,
    maxZoom: 13,
    keyboard: false,
    zoomControl: false,
    attributionControl: true,
  });
  el.removeAttribute("tabindex");
  mapa.attributionControl.setPrefix(false);
  mapa.attributionControl.addAttribution('Malha territorial: <a href="https://www.ibge.gov.br/">IBGE</a>');

  /* O contorno preenchido do Estado fica numa pane propria, abaixo das
     outras: na mesma pane, a ordem entre o SVG dele e o canvas das divisas
     nao e garantida, e o preenchimento poderia cobrir as divisas. */
  mapa.createPane("base");
  mapa.getPane("base").style.zIndex = 300;

  const estado = L.geoJSON(window.SP_GEOJSON, {
    pane: "base",
    style: {
      color: token("--c-line", "#54546D"),
      weight: 1.5,
      fillColor: token("--c-surface", "#2A2A37"),
      fillOpacity: 1,
    },
    interactive: false,
  }).addTo(mapa);

  if (window.SP_MUNICIPIOS) {
    L.geoJSON(window.SP_MUNICIPIOS, {
      renderer: L.canvas({ padding: 0.3 }),
      style: { color: token("--c-line", "#54546D"), weight: 0.6, fill: false },
      interactive: false,
    }).addTo(mapa);
  }

  mapa.fitBounds(estado.getBounds(), { padding: [8, 8] });

  const marcador = L.circleMarker([0, 0], {
    radius: 8,
    color: token("--c-bg", "#1F1F28"),
    weight: 2,
    fillColor: token("--c-accent", "#7E9CD8"),
    fillOpacity: 1,
    interactive: false,
  });

  function lerNumero(campo) {
    const texto = campo.value.trim().replace(",", ".");
    if (texto === "") return NaN;
    return Number(texto);
  }

  function escrever(campo, valor) {
    campo.value = valor.toFixed(5).replace(".", ",");
    /* Avisa quem escuta o campo (validacao, HTMX) que o valor mudou. */
    campo.dispatchEvent(new Event("input", { bubbles: true }));
  }

  /* Municipio que contem o ponto: primeiro pela caixa de cada um, que e
     barata, e so depois pelo poligono. Cada municipio vira uma malha de uma
     feicao so, sempre o mesmo objeto, para o ClimaCampo guardar as arestas
     dele entre um clique e outro. */
  const malhas = new WeakMap();
  function malhaDe(feicao) {
    if (!malhas.has(feicao)) malhas.set(feicao, { features: [feicao] });
    return malhas.get(feicao);
  }

  function municipioEm(lat, lon) {
    const dados = window.SP_MUNICIPIOS;
    if (!dados || typeof ClimaCampo === "undefined") return null;
    for (const f of dados.features) {
      const b = f.properties.bbox;
      if (lon < b[0] || lat < b[1] || lon > b[2] || lat > b[3]) continue;
      if (ClimaCampo.dentroDaMalha(malhaDe(f), lat, lon)) return f.properties.nome;
    }
    return null;
  }

  function atualizar(centralizar) {
    const lat = lerNumero(campoLat);
    const lon = lerNumero(campoLon);
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) {
      marcador.remove();
      if (aviso) aviso.textContent = "";
      return;
    }
    marcador.setLatLng([lat, lon]).addTo(mapa);
    if (centralizar) mapa.setView([lat, lon], Math.max(mapa.getZoom(), 9));

    if (!aviso) return;
    const nome = municipioEm(lat, lon);
    aviso.textContent = nome
      ? "Ponto em " + nome + "."
      : "Este ponto fica fora do Estado de São Paulo.";
  }

  mapa.on("click", function (evento) {
    escrever(campoLat, evento.latlng.lat);
    escrever(campoLon, evento.latlng.lng);
    atualizar(false);
  });

  campoLat.addEventListener("change", function () { atualizar(true); });
  campoLon.addEventListener("change", function () { atualizar(true); });

  /* Na edicao os campos ja vem preenchidos. */
  atualizar(true);
})();
