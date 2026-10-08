package lock

// Event is something that happened to the lockfile. Use cases pull events
// after a change and hand them to subscribers (operation log, CLI output).
type Event interface {
	isLockEvent()
}

// SkillInstalled is recorded by Lockfile.Install.
type SkillInstalled struct{ Entry Entry }

// SkillUpdated is recorded by Lockfile.Update.
type SkillUpdated struct{ Previous, Current Entry }

// SkillRemoved is recorded by Lockfile.Remove.
type SkillRemoved struct{ Entry Entry }

func (SkillInstalled) isLockEvent() {}
func (SkillUpdated) isLockEvent()   {}
func (SkillRemoved) isLockEvent()   {}
