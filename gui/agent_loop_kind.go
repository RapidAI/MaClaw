package main

// LoopKind distinguishes front-end chat loops from background loops.
type LoopKind int

const (
	LoopKindChat       LoopKind = iota // interactive user chat
	LoopKindBackground                 // background task (coding, scheduled, auto)
)

const LoopKindNormal = LoopKindChat
