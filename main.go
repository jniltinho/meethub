// Command meethub is the MeetHub server and administration CLI: team chat with
// LiveKit meetings in a single binary. It delegates to cmd.Execute (Cobra).
package main

import "github.com/jniltinho/meethub/cmd"

func main() {
	cmd.Execute()
}
