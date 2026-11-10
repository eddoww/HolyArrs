package subtext

import (
	"strconv"
	"strings"
	"testing"
)

// srtOf makes an SRT of the lines, one cue each.
func srtOf(lines ...string) string {
	var b strings.Builder
	for i, l := range lines {
		b.WriteString(strconv.Itoa(i+1) + "\n00:00:01,000 --> 00:00:02,000\n<i>" + l + "</i>\n\n")
	}
	return b.String()
}

// TestGuessLanguage reads a clear language and refuses short, mixed and
// unknown text. The first case is shaped like a real language-less YTS
// sidecar: two lines of ads, then a few forced lines.
func TestGuessLanguage(t *testing.T) {
	for _, c := range []struct {
		why, text, format, want string
	}{
		{"a short English sidecar", srtOf("Downloaded from", "YTS.MX", "Official movies site:", "I am a butterfly...", "...And I must fly!",
			"A butterfly? Well then, I must not keep you from flying!", "It's a boy.", "There are two.", "A girl..."), SRT, "en"},
		{"English", srtOf("What do you want from me?", "I know that you were there.", "They will be here, just like she said.", "Well, then we must go."), SRT, "en"},
		{"Dutch", srtOf("Ik weet niet wat je wilt.", "Het is een mooie dag voor ons.", "Maar hij moet naar huis gaan.", "Waar zijn jullie nu?"), SRT, "nl"},
		{"Norwegian", srtOf("Hva vil du meg?", "Jeg vet ikke hva som skjedde nå.", "Kanskje noen kan hjelpe deg etter skolen.", "Nei, det ble for mye.", "Hvorfor sier du noe sånn?"), SRT, "no"},
		{"Danish", srtOf("Hvad vil du mig?", "Jeg ved ikke noget om det nu.", "Måske kan nogen hjælpe dig efter skolen.", "Nej, det blev for meget.", "Hvad mener du?", "Hvad laver du? Det er noget helt andet, måske."), SRT, "da"},
		{"Swedish", srtOf("Vad vill du mig?", "Jag vet inte något om det nu.", "Kanske kan någon hjälpa dig efter skolan.", "Nej, det blev för mycket.", "Varför säger du så?"), SRT, "sv"},
		{"German in ASS", "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\\i1}Ich weiß nicht, was du willst.\\NAber das ist nicht mein Problem.\n" +
			"Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,Wir sind jetzt hier, und sie auch.\nDialogue: 0,0:00:05.00,0:00:06.00,Default,,0,0,0,,Wenn du schon da bist, kann ich nur warten.\n", ASS, "de"},
		{"too short", srtOf("Hello there, you."), SRT, ""},
		{"half English, half Dutch", srtOf("What do you want from me?", "I know that you were there.", "Ik weet niet wat je wilt.", "Het is een mooie dag voor ons.", "Maar hij moet naar huis gaan."), SRT, ""},
		{"a language without words here", srtOf("Kochana, gdzie jesteś?", "Nie wiem, co się stało.", "Proszę, powiedz mi prawdę."), SRT, ""},
		{"only timings and numbers", "1\n00:00:01,000 --> 00:00:02,000\n42\n", SRT, ""},
	} {
		if got := GuessLanguage(c.text, c.format); got != c.want {
			t.Errorf("%s: %q, want %q", c.why, got, c.want)
		}
	}
}
