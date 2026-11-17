/* ==========================================================================
   auth.js — melhorias progressivas das paginas de login e cadastro.
   Sem este arquivo os formularios continuam funcionando: a validacao e a
   nativa do HTML5 e o botao "Mostrar" permanece oculto.
   ========================================================================== */

(function () {
    "use strict";

    /* --- Mostrar / ocultar senha --- */
    document.querySelectorAll("[data-toggle-senha]").forEach(function (botao) {
        var campo = document.getElementById(botao.dataset.toggleSenha);
        if (!campo) {
            return;
        }

        botao.hidden = false;

        botao.addEventListener("click", function () {
            var visivel = campo.type === "text";
            campo.type = visivel ? "password" : "text";
            botao.textContent = visivel ? "Mostrar" : "Ocultar";
            campo.focus();
        });
    });

    /* --- Confirmacao de senha ---
       setCustomValidity marca o campo como invalido, entao :user-invalid e a
       mensagem do navegador funcionam igual aos outros erros do formulario. */
    var confirma = document.querySelector("[data-confirma-senha]");

    if (confirma) {
        var original = document.getElementById(confirma.dataset.confirmaSenha);

        if (original) {
            var verificar = function () {
                var iguais = confirma.value === original.value;
                confirma.setCustomValidity(iguais ? "" : "As senhas não são iguais.");
            };

            confirma.addEventListener("input", verificar);
            original.addEventListener("input", verificar);
        }
    }

    /* --- Formularios ainda sem rota no servidor ---
       As paginas ja sao servidas pelo backend, mas o cadastro, o login e as
       rotas protegidas entram com a etapa de autenticacao. Ate la o envio e
       interceptado e o motivo fica escrito na tela, em vez de o formulario
       recarregar vazio -- o que pareceria uma falha silenciosa.

       O preventDefault vem depois da validacao nativa: o navegador so dispara
       submit em formulario valido, entao os campos obrigatorios, o formato do
       e-mail e a confirmacao de senha continuam sendo cobrados antes do aviso. */
    var TEXTO_AVISO =
        "Cadastro e login ainda não estão ligados ao servidor: a " +
        "autenticação entra na próxima etapa do projeto. O mapa e as " +
        "medições estão abertos a todos, sem conta.";

    document.querySelectorAll(".auth-form").forEach(function (formulario) {
        formulario.addEventListener("submit", function (evento) {
            evento.preventDefault();

            var aviso = formulario.querySelector(".auth-aviso");
            if (!aviso) {
                return;
            }

            aviso.textContent = TEXTO_AVISO;
            aviso.hidden = false;

            /* O aviso e a resposta ao clique, entao precisa estar a vista:
               em telas baixas o botao pode ser a ultima coisa visivel. */
            aviso.scrollIntoView({ block: "nearest" });
        });
    });
})();
