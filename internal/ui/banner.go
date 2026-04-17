// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package ui

// Banner prints the DevContract ASCII art banner.
func Banner(version string) string {
	brand := StyleBrand.Render

	art := brand("  ╔═══════════════════════════════╗") + "\n" +
		brand("  ║") + "  " + StyleBold.Render("DevContract") + "  " + StyleDim.Render(version) + brand("          ║") + "\n" +
		brand("  ║") + "  " + StyleDim.Render("Secure .env sync for teams") + brand("  ║") + "\n" +
		brand("  ╚═══════════════════════════════╝")

	return art
}

// Header prints a formatted section header.
func Header(title string) {
	printOutln("")
	printOutln(Indent(BrandIcon() + " " + StyleBold.Render(title)))
	printOutln("")
}

// Status prints a status line with an icon.
func Status(msg string) {
	printOutln(Indent(InfoIcon() + " " + msg))
}

// Success prints a success line.
func Success(msg string) {
	printOutln(Indent(SuccessIcon() + " " + msg))
}

// Warning prints a warning line.
func Warning(msg string) {
	printOutln(Indent(WarningIcon() + " " + StyleWarning.Render(msg)))
}

// Error prints an error line.
func Error(msg string) {
	printErrln(Indent(ErrorIcon() + " " + StyleError.Render(msg)))
}

// Blank prints a blank line.
func Blank() {
	printOutln("")
}

// Line prints the given text indented.
func Line(text string) {
	printOutln(Indent(text))
}

// Code prints text styled as code.
func Code(text string) {
	printOutln(Indent(StyleCode.Render(text)))
}
