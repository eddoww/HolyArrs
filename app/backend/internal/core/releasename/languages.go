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
// a Release name's tags (langWords count too). Short words that clash with
// names (pl) stay out, as upstream keeps them; hun counts only with a tag on
// both sides, as upstream reads it. They are read only after the title, so a title such
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
	"dutch": "nl", "flemish": "nl", "swe": "sv", "cze": "cs", "heb": "he",
	"danish": "da", "swedish": "sv", "finnish": "fi", "icelandic": "is",
	"norwegian": "no", "norsk": "no", "norske": "no", "nor": "no", "bokmal": "no", "nynorsk": "no",
	"chinese": "zh", "mandarin": "zh", "cantonese": "zh",
	"polish": "pl", "czech": "cs", "slovak": "sk", "hungarian": "hu", "hundub": "hu",
	"hebrew": "he", "hebdub": "he", "hebdubbed": "he",
	"hindi": "hi", "tamil": "ta", "telugu": "te", "tel": "te", "malayalam": "ml", "kannada": "kn", "marathi": "mr",
	"portuguese": "pt", "brazilian": "pt-br", "dublado": "pt-br", "pt-br": "pt-br",
	"ukrainian": "uk", "ukr": "uk", "greek": "el", "turkish": "tr", "arabic": "ar", "thai": "th",
	"vietnamese": "vi", "vie": "vi", "romanian": "ro", "rodubbed": "ro", "bulgarian": "bg", "bgaudio": "bg",
	"persian": "fa", "bengali": "bn", "latvian": "lv", "lithuanian": "lt", "catalan": "ca", "urdu": "ur",
	"irish": "ga", "georgian": "ka", "mongolian": "mn", "romansh": "rm", "albanian": "sq", "afrikaans": "af",
	"tagalog": "tl", "croatian": "hr", "serbian": "sr", "bosnian": "bs", "estonian": "et", "indonesian": "id",
	"macedonian": "mk", "slovenian": "sl",
}

// dubWords name a dub and are no ordinary word, so they count whatever
// follows ("Dublado Portugues").
var dubWords = setOf("dublado", "latino", "castellano", "truefrench", "vff", "vfq", "vfi", "vf2", "hundub", "hebdub", "hebdubbed", "rodubbed", "bgaudio")

// nameLangUpper are short words that name a language only in capitals
// ("EN/FR/ES"); in lower case they are too often other words.
var nameLangUpper = map[string]string{"EN": "en", "FR": "fr", "ES": "es", "DE": "de", "CZ": "cs", "LT": "lt", "BG": "bg", "SK": "sk"}

// nordicLangs are the dubs a NORDiC release carries besides the original.
var nordicLangs = []string{"da", "fi", "no", "sv"}

// readForkLanguages adds, after upstream's reading (toks as written, low in
// lower case, both already split at "/" and cut before subtitle words), what
// the fork reads on top, as Sonarr and Radarr do: NORDiC holds the Nordic
// dubs and the original; nor, norsk and other words upstream leaves out; a
// dub word whatever follows it; EN/FR/ES in capitals; joined words
// ("NOR-ENG", "Hindi+English"); and the regional codes Sonarr and Radarr keep
// apart (LATINO es-419, Brazilian pt-br).
func readForkLanguages(r *Result, toks, low []string, flags map[string]bool, add func(string)) {
	isSub := func(i int) bool { return i >= 0 && i < len(low) && subWord(low[i]) }
	for i, t := range low {
		switch {
		case t == "nordic":
			for _, c := range nordicLangs {
				add(c)
			}
			flags["original"] = true
			continue
		case t == "multiaudio" || (t == "multi" && i+1 < len(low) && low[i+1] == "audio"):
			flags["multi"] = true
		case t == "pt" && i+1 < len(low) && low[i+1] == "br":
			add("pt-br")
		}
		if t == "hun" {
			continue // upstream's rule only: "The.Hun.720p" stays a title word
		}
		if code, ok := nameLangWords[t]; ok {
			if (dubWords[t] || i+1 == len(low) || !plainWord(low[i+1])) && !isSub(i+1) {
				add(code)
			}
			continue
		}
		if code, ok := nameLangUpper[toks[i]]; ok && !(toks[i] == "ES" && i > 0 && low[i-1] == "dts") && !isSub(i-1) && !isSub(i+1) {
			add(code)
		}
		// Joined words: "ENG-ITA", "NOR-ENG" and "Hindi+English" are not
		// split by the separators.
		if strings.ContainsAny(t, "-+") {
			parts := strings.FieldsFunc(toks[i], func(c rune) bool { return c == '-' || c == '+' })
			if i == len(low)-1 && strings.Contains(toks[i], "-") && len(parts) > 1 {
				// The last word's part after its dash is the release group
				// ("H.264-Japanese"), not a language.
				parts = parts[:len(parts)-1]
			}
			for j, p := range parts {
				if code, ok := nameLangWords[strings.ToLower(p)]; ok {
					add(code)
				} else if code, ok := nameLangUpper[p]; ok && !(p == "ES" && j > 0 && strings.EqualFold(parts[j-1], "dts")) {
					add(code)
				}
			}
		}
	}
	// Regional dubs: Sonarr and Radarr keep Latin American Spanish and
	// Brazilian Portuguese apart, so a name that only says LATINO or
	// Brazilian holds that, not the plain language upstream reads.
	regional(r, low, "es-419", "es", "spanish", "spa", "castellano", "espanol", "español", "esp", "es")
	regional(r, low, "pt-br", "pt", "portuguese", "por", "pt")
}

// regional drops the plain code upstream read for a regional dub (code)
// when no word names the plain language itself (plainWords).
func regional(r *Result, low []string, code, plain string, plainWords ...string) {
	if !slices.Contains(r.Languages, code) || !slices.Contains(r.Languages, plain) {
		return
	}
	for i, t := range low {
		if slices.Contains(plainWords, t) && !(t == "pt" && i+1 < len(low) && low[i+1] == "br") {
			return
		}
	}
	r.Languages = slices.DeleteFunc(r.Languages, func(c string) bool { return c == plain })
}

// subWord is a word that marks subtitles.
func subWord(t string) bool {
	return t == "sub" || t == "subs" || t == "subtitle" || t == "subtitles"
}

// LanguageName is a language code's English name, or the code itself.
func LanguageName(code string) string {
	if n, ok := LanguageNames[code]; ok {
		return n
	}
	return code
}
