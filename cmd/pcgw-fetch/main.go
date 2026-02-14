// Command pcgw-fetch fetches save location templates from PCGamingWiki for a game (by Steam App ID).
// Usage: pcgw-fetch <steam_app_id>
// Output: JSON array of SaveLocationTemplate to stdout (or add to a local DB).
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gsbs/gsbs/pkg/pcgw"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pcgw-fetch <steam_app_id>")
		os.Exit(1)
	}
	appID := os.Args[1]
	client := pcgw.NewClient()
	pageID, err := client.GetPageIDBySteamAppID(appID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "get page id:", err)
		os.Exit(1)
	}
	wikitext, err := client.ParsePageWikitext(pageID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse wikitext:", err)
		os.Exit(1)
	}
	templates := pcgw.ParseSaveLocationsFromWikitext(wikitext, appID)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(templates); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
