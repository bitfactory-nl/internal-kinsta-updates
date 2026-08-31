package domain

// AISQLAntwoord is wat de AI teruggeeft op een vraag over de database. Het is
// een voorstel: er wordt niets uitgevoerd tot de gebruiker daar apart op klikt.
type AISQLAntwoord struct {
	// SQL is precies één statement.
	SQL string `json:"sql"`
	// Uitleg beschrijft in gewone taal wat de query doet.
	Uitleg string `json:"uitleg"`
	// Aannames zijn de gaten die de AI zelf heeft gedicht — welke tabel hij als
	// de bedoelde las, hoe hij "recent" heeft opgevat. Dit is het eerste waar je
	// naar kijkt als het antwoord niet klopt.
	Aannames []string `json:"aannames"`
	// Waarschuwing is de eigen opmerking van de AI, bijvoorbeeld dat de query
	// rijen wijzigt of dat de vraag meerdere kanten op kon.
	Waarschuwing string `json:"waarschuwing"`
}
