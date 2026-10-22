package awake

// caffeinate -i stops the idle sleep, and -s the sleep of the system on mains power.
var holdCommand = []string{"caffeinate", "-i", "-s"}

func holdArgs(string) []string { return nil }
