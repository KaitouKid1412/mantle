package render

import "image/color"

func rgb(hex uint32) color.Color {
	return color.RGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}

// testPalette is a fixed palette for goldens; its values are arbitrary.
var testPalette = MapPalette{
	TokText:       rgb(0xdddddd),
	TokInactive:   rgb(0x888888),
	TokSubtle:     rgb(0x555555),
	TokAccent:     rgb(0xd08050),
	TokPermission: rgb(0x8899ff),
	TokSuccess:    rgb(0x55bb55),
	TokError:      rgb(0xee5555),
	TokWarning:    rgb(0xddaa33),

	TokDiffAdded:       rgb(0x1f4a2a),
	TokDiffRemoved:     rgb(0x5a2626),
	TokDiffAddedWord:   rgb(0x2f7a40),
	TokDiffRemovedWord: rgb(0x8a3434),

	TokSyntaxKeyword:     rgb(0xc678dd),
	TokSyntaxString:      rgb(0x98c379),
	TokSyntaxNumber:      rgb(0xd19a66),
	TokSyntaxComment:     rgb(0x7f848e),
	TokSyntaxFunction:    rgb(0x61afef),
	TokSyntaxType:        rgb(0xe5c07b),
	TokSyntaxVariable:    rgb(0xe06c75),
	TokSyntaxConstant:    rgb(0xd19a66),
	TokSyntaxOperator:    rgb(0x56b6c2),
	TokSyntaxPunctuation: rgb(0xabb2bf),
	TokSyntaxTag:         rgb(0xe06c75),
	TokSyntaxAttribute:   rgb(0xd19a66),
	TokSyntaxBuiltin:     rgb(0x56b6c2),
	TokSyntaxPlain:       rgb(0xabb2bf),
}
