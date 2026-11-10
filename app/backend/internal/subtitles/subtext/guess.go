package subtext

import (
	"strings"
	"unicode"
)

// guessWords are frequent short words of each language that GuessLanguage
// counts. A word on two lists says nothing and is dropped (guessUnique), so
// close languages such as Norwegian, Danish and Swedish are told apart only
// by the words they do not share. Some lists also hold a frequent word that
// is common in another language ("for", "der", "et" in Danish), only so that
// it drops out there too.
var guessWords = map[string]string{
	"en": "the and you that what this have with not are was for but they there will would your just know from all she him her about here like want well then must am it me my we be do of to he",
	"nl": "de het een van ik je niet dat wat zijn met voor maar hij ze er op te dit naar ook nog wel kan heb hebt mijn jij jullie hier waar als om zo geen moet gaan weet",
	"no": "jeg ikke det og er på til du har med som av han vi hva kan var så meg deg den om nå noe bare skal vil hvor hvordan her ja nei oss dem hun ble kanskje også etter mye hvorfor noen sånn ikkje eg for to der et dit se da ha kun be",
	"da": "jeg ikke det og er på til du har med som af han vi hvad kan var så mig dig den om nu noget bare skal vil hvor hvordan her ja nej os dem hun blev måske også efter meget hvorfor nogen for have to der mit et op dit se da kun",
	"sv": "jag inte det och är på till du har med som av han vi vad kan var så mig dig den om nu något bara ska vill hur här ja nej oss dem hon blev kanske också efter mycket varför någon för se da ha be",
	"de": "der die das und ich nicht ist du ein eine zu es sie wir mit auf sich dass was aber ja nein wie mir mich dich hier noch auch nur kann habe hast bin bist wenn jetzt schon sein am als",
	"fr": "le la les et je tu vous il elle nous est pas ne un une de des du que qui ce ça pour dans avec mais oui non sur moi toi suis êtes bien fait rien tout très me te se",
	"es": "el los las que y es no un una por para con pero qué sí yo tú usted eso esto está estoy aquí como muy bien nada todo señor vamos hay mi su lo del al me te se le",
	"it": "il la che e di non un una per con ma sono sei è io tu lui lei noi voi questo quello cosa come qui bene niente tutto molto perché anche ancora ho hai ha mi ti ci gli della del me te se",
	"pt": "o os as que e não um uma por para com mas eu você ele ela nós isso isto está estou aqui como muito bem nada tudo sim obrigado vamos há meu minha seu do da dos das no na",
	"fi": "ja on ei se että minä sinä hän me te he mitä mutta kun niin nyt tämä joka ole olen olet oli vain kaikki mikä missä miksi kanssa hyvä kiitos",
}

// guessUnique maps each word on exactly one list to its language.
var guessUnique = func() map[string]string {
	seen := map[string]map[string]bool{}
	for lang, words := range guessWords {
		for _, w := range strings.Fields(words) {
			if seen[w] == nil {
				seen[w] = map[string]bool{}
			}
			seen[w][lang] = true
		}
	}
	out := map[string]string{}
	for w, langs := range seen {
		if len(langs) == 1 {
			for l := range langs {
				out[w] = l
			}
		}
	}
	return out
}()

// Thresholds for a clear guess: enough counted words, and the best language
// far ahead of the next.
const (
	guessMinWords = 8
	guessLead     = 3
)

// GuessLanguage guesses the language of subtitle text (already Clean) from
// its spoken lines: the code of the one language whose words are clearly the
// most common, or "" when the text is too short, mixed or in a language it
// has no words for.
func GuessLanguage(text, format string) string {
	counts := map[string]int{}
	for line := range strings.Lines(text) {
		line = spokenPart(strings.TrimSpace(line), format)
		for _, w := range strings.FieldsFunc(strings.ToLower(line), func(r rune) bool { return !unicode.IsLetter(r) }) {
			if l, ok := guessUnique[w]; ok {
				counts[l]++
			}
		}
	}
	best, first, second := "", 0, 0
	for l, n := range counts {
		switch {
		case n > first:
			best, first, second = l, n, first
		case n > second:
			second = n
		}
	}
	if first < guessMinWords || first < guessLead*second {
		return ""
	}
	return best
}

// spokenPart is the text of a line that may be spoken: cue numbers, timings
// and the ASS and SSA field list are left out, and {...} and <...> codes are
// removed.
func spokenPart(line, format string) string {
	switch {
	case line == "" || strings.Contains(line, "-->") || strings.HasPrefix(line, "WEBVTT"):
		return ""
	case format == ASS || format == SSA:
		rest, ok := strings.CutPrefix(line, "Dialogue:")
		if !ok {
			return ""
		}
		parts := strings.SplitN(rest, ",", 10)
		if len(parts) < 10 {
			return ""
		}
		line = strings.NewReplacer(`\N`, " ", `\n`, " ", `\h`, " ").Replace(parts[9])
	}
	var b strings.Builder
	depth := 0
	for _, r := range line {
		switch r {
		case '{', '<':
			depth++
		case '}', '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
