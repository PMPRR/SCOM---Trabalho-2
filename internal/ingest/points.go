package ingest

// SaoPauloPoints are the places where the Open-Meteo model is sampled: the
// same localities the map of Trabalho I used, spread over the regions of the
// State so the interpolated field has support everywhere.
var SaoPauloPoints = []Point{
	{Code: "jales", Name: "Jales", Lat: -20.2687, Lon: -50.5456},
	{Code: "sao-jose-do-rio-preto", Name: "São José do Rio Preto", Lat: -20.8113, Lon: -49.3758},
	{Code: "aracatuba", Name: "Araçatuba", Lat: -21.2079, Lon: -50.4325},
	{Code: "presidente-epitacio", Name: "Presidente Epitácio", Lat: -21.7633, Lon: -52.1097},
	{Code: "presidente-prudente", Name: "Presidente Prudente", Lat: -22.1207, Lon: -51.3889},
	{Code: "marilia", Name: "Marília", Lat: -22.2171, Lon: -49.9501},
	{Code: "barretos", Name: "Barretos", Lat: -20.5570, Lon: -48.5695},
	{Code: "ribeirao-preto", Name: "Ribeirão Preto", Lat: -21.1775, Lon: -47.8103},
	{Code: "franca", Name: "Franca", Lat: -20.5390, Lon: -47.4010},
	{Code: "bauru", Name: "Bauru", Lat: -22.3145, Lon: -49.0606},
	{Code: "araraquara", Name: "Araraquara", Lat: -21.7845, Lon: -48.1780},
	{Code: "sao-carlos", Name: "São Carlos", Lat: -22.0087, Lon: -47.8909},
	{Code: "piracicaba", Name: "Piracicaba", Lat: -22.7253, Lon: -47.6492},
	{Code: "campinas", Name: "Campinas", Lat: -22.8184, Lon: -47.0647},
	{Code: "ourinhos", Name: "Ourinhos", Lat: -22.9795, Lon: -49.8695},
	{Code: "avare", Name: "Avaré", Lat: -23.0986, Lon: -48.9263},
	{Code: "sorocaba", Name: "Sorocaba", Lat: -23.5015, Lon: -47.4526},
	{Code: "sao-paulo", Name: "São Paulo", Lat: -23.4961, Lon: -46.6199},
	{Code: "itapetininga", Name: "Itapetininga", Lat: -23.5917, Lon: -48.0533},
	{Code: "braganca-paulista", Name: "Bragança Paulista", Lat: -22.9526, Lon: -46.5417},
	{Code: "sao-jose-dos-campos", Name: "São José dos Campos", Lat: -23.1896, Lon: -45.8841},
	{Code: "taubate", Name: "Taubaté", Lat: -23.0264, Lon: -45.5553},
	{Code: "campos-do-jordao", Name: "Campos do Jordão", Lat: -22.7397, Lon: -45.5914},
	{Code: "ubatuba", Name: "Ubatuba", Lat: -23.4336, Lon: -45.0838},
	{Code: "santos", Name: "Santos", Lat: -23.9608, Lon: -46.3336},
	{Code: "registro", Name: "Registro", Lat: -24.4871, Lon: -47.8442},
	{Code: "cananeia", Name: "Cananéia", Lat: -25.0148, Lon: -47.9268},
	{Code: "itapeva", Name: "Itapeva", Lat: -23.9821, Lon: -48.8758},
}
