package releasename

import (
	"slices"
	"strings"
)

// LanguageNames are the languages HolyArrs reads from names and judges by,
// keyed by code: ISO 639-1, with "pt-br" for Brazilian Portuguese, "es-419"
// for Latin American Spanish and "nl-be" for Flemish, as Sonarr and Radarr
// keep those apart.
var LanguageNames = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "de": "German", "it": "Italian", "da": "Danish",
	"nl": "Dutch", "ja": "Japanese", "is": "Icelandic", "zh": "Chinese", "ru": "Russian", "pl": "Polish",
	"vi": "Vietnamese", "sv": "Swedish", "no": "Norwegian", "fi": "Finnish", "tr": "Turkish", "pt": "Portuguese",
	"nl-be": "Flemish", "el": "Greek", "ko": "Korean", "hu": "Hungarian", "he": "Hebrew", "lt": "Lithuanian",
	"cs": "Czech", "hi": "Hindi", "ro": "Romanian", "th": "Thai", "bg": "Bulgarian", "pt-br": "Portuguese (Brazil)",
	"ar": "Arabic", "uk": "Ukrainian", "fa": "Persian", "bn": "Bengali", "sk": "Slovak", "lv": "Latvian",
	"es-419": "Spanish (Latino)", "ca": "Catalan", "hr": "Croatian", "sr": "Serbian", "bs": "Bosnian",
	"et": "Estonian", "ta": "Tamil", "id": "Indonesian", "te": "Telugu", "mk": "Macedonian", "sl": "Slovenian",
	"ml": "Malayalam", "kn": "Kannada", "sq": "Albanian", "af": "Afrikaans", "mr": "Marathi", "tl": "Tagalog",
	"ur": "Urdu", "rm": "Romansh", "mn": "Mongolian", "ka": "Georgian", "ga": "Irish",
}

// nameLangWords are the words, in lower case, that name an audio language in
// a Release name's tags. They are read only after the title, so a title such
// as "The Danish Girl" keeps its words. Words for subtitles (SWESUB,
// SUBFRENCH) are single words and so never match.
var nameLangWords = map[string]string{
	"english": "en", "eng": "en",
	"french": "fr", "truefrench": "fr", "fre": "fr", "fra": "fr", "vf": "fr", "vff": "fr", "vfq": "fr", "vfi": "fr", "vf2": "fr",
	"german": "de", "ger": "de", "swissgerman": "de", "videomann": "de",
	"italian": "it", "ita": "it",
	"spanish": "es", "spa": "es", "castellano": "es", "espanol": "es", "español": "es",
	"latino":  "es-419",
	"russian": "ru", "rus": "ru",
	"japanese": "ja", "jpn": "ja", "jap": "ja",
	"korean": "ko", "kor": "ko",
	"dutch": "nl", "flemish": "nl-be",
	"danish": "da", "swedish": "sv", "finnish": "fi", "icelandic": "is",
	"norwegian": "no", "norsk": "no", "norske": "no", "nor": "no", "bokmal": "no", "nynorsk": "no",
	"chinese": "zh", "mandarin": "zh", "cantonese": "zh",
	"polish": "pl", "czech": "cs", "slovak": "sk", "hungarian": "hu", "hun": "hu", "hundub": "hu",
	"hebrew": "he", "hebdub": "he", "hebdubbed": "he",
	"hindi": "hi", "tamil": "ta", "telugu": "te", "tel": "te", "malayalam": "ml", "kannada": "kn", "marathi": "mr",
	"portuguese": "pt", "brazilian": "pt-br", "dublado": "pt-br",
	"ukrainian": "uk", "ukr": "uk", "greek": "el", "turkish": "tr", "arabic": "ar", "thai": "th",
	"vietnamese": "vi", "vie": "vi", "romanian": "ro", "rodubbed": "ro", "bulgarian": "bg", "bgaudio": "bg",
	"persian": "fa", "bengali": "bn", "latvian": "lv", "lithuanian": "lt", "catalan": "ca", "urdu": "ur",
	"irish": "ga", "georgian": "ka", "mongolian": "mn", "romansh": "rm", "albanian": "sq", "afrikaans": "af",
	"tagalog": "tl", "croatian": "hr", "serbian": "sr", "bosnian": "bs", "estonian": "et", "indonesian": "id",
	"macedonian": "mk", "slovenian": "sl",
}

// nameLangUpper are short words that name a language only in capitals
// ("EN/FR/ES"); in lower case they are too often other words.
var nameLangUpper = map[string]string{"EN": "en", "FR": "fr", "ES": "es", "DE": "de", "PL": "pl", "CZ": "cs", "LT": "lt", "BG": "bg", "SK": "sk"}

// nordicLangs are the dubs a NORDiC release carries besides the original.
var nordicLangs = []string{"da", "fi", "no", "sv"}

// readLanguages reads the audio languages and the multi-audio flags from a
// Release name's tags (raw is as written, low in lower case). NORDiC holds
// the Nordic dubs and the original; ORIGINAL or ORIG holds the original
// (flag original_audio, as the title's language is not known here).
func readLanguages(r *Result, raw, low []string, flags map[string]bool) {
	add := func(code string) {
		if !slices.Contains(r.Languages, code) {
			r.Languages = append(r.Languages, code)
		}
	}
	germanDL, germanML := false, false
	for i, t := range low {
		switch {
		case t == "dl" && (i == 0 || low[i-1] != "web"):
			germanDL = true
		case t == "ml":
			germanML = true
		case t == "nordic":
			for _, c := range nordicLangs {
				add(c)
			}
			flags["original_audio"] = true
		case t == "original" || t == "orig":
			flags["original_audio"] = true
		case t == "multiaudio" || (t == "multi" && i+1 < len(low) && low[i+1] == "audio"):
			flags["multi"] = true
		case t == "pt" && i+1 < len(low) && low[i+1] == "br":
			add("pt-br")
		}
		if code, ok := nameLangWords[t]; ok {
			add(code)
			continue
		}
		if code, ok := nameLangUpper[raw[i]]; ok && !(raw[i] == "ES" && i > 0 && low[i-1] == "dts") {
			add(code)
		}
		// Joined words: "Ita-Eng", "EN/FR/ES" and "[Hindi+English]" are split
		// by the separators already; "ENG-ITA" and "DUB-PL" are not.
		if strings.ContainsAny(t, "-+/") {
			parts := strings.FieldsFunc(raw[i], func(c rune) bool { return c == '-' || c == '+' || c == '/' })
			for j, p := range parts {
				if code, ok := nameLangWords[strings.ToLower(p)]; ok {
					add(code)
				} else if code, ok := nameLangUpper[p]; ok && !(p == "ES" && j > 0 && strings.EqualFold(parts[j-1], "dts")) {
					add(code)
				}
			}
		}
	}
	germanDual(r, germanDL, germanML, flags)
}

// germanDual applies the German scene tags: a German release marked DL
// also holds the original, one marked ML the original and English.
func germanDual(r *Result, dl, ml bool, flags map[string]bool) {
	if !slices.Equal(r.Languages, []string{"de"}) {
		return
	}
	switch {
	case dl:
		flags["original_audio"] = true
	case ml:
		flags["original_audio"] = true
		r.Languages = append(r.Languages, "en")
	}
}

// LanguageName is a language code's English name, or the code itself.
func LanguageName(code string) string {
	if n, ok := LanguageNames[code]; ok {
		return n
	}
	return code
}
