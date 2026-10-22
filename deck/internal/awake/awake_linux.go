package awake

var holdCommand = []string{"systemd-inhibit", "--what=sleep:idle", "--who=Printdeck", "--mode=block"}

func holdArgs(why string) []string { return []string{"--why=" + why, "sleep", "infinity"} }
