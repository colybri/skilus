package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain"
)

// auto is the argument that clears the setting so the system decides.
const auto = "auto"

func newLangCommand(h app.LanguageSetting, choice i18n.Choice, t *i18n.Catalog) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "lang",
		Aliases: []string{"language"},
		Short:   t.T("Muestra o cambia el idioma en que skilus escribe"),
		Long: t.T(`Sin argumentos, muestra el idioma que usa skilus, de dónde sale y los
disponibles. Por defecto sigue al sistema: LANGUAGE, LC_ALL, LC_MESSAGES y
LANG (en Windows, el idioma del usuario). Si ninguno es uno de los
disponibles, usa el inglés. La variable SKILUS_LANG manda sobre todo lo
demás.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			renderLanguage(cmd.OutOrStdout(), choice, t)
			return nil
		},
	}
	set := &cobra.Command{
		Use:   t.T("set <idioma>"),
		Short: t.T("Cambia el idioma de skilus para tu usuario"),
		Long: t.T(`Guarda el idioma en ~/.skilus/config.yaml. Acepta un código (es, en, fr…)
o un locale como pt_BR.UTF-8; auto vuelve a seguir al sistema.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := ""
			if !strings.EqualFold(args[0], auto) {
				c, ok := i18n.Match(args[0])
				if !ok {
					return fmt.Errorf("%s: %w", t.T("idioma no disponible: %s; elige uno de %s", args[0], languageCodes()), domain.ErrInvalid)
				}
				code = c
			}
			if err := h.Set(cmd.Context(), code); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if code == "" {
				fmt.Fprintln(out, t.T("skilus seguirá el idioma del sistema."))
				return nil
			}
			// Confirm in the language just chosen.
			nt, err := i18n.Load(code)
			if err != nil {
				return err
			}
			l, _ := i18n.Lookup(code)
			fmt.Fprint(out, nt.T("Idioma cambiado a %s (%s).\n", l.Name, l.Code))
			if choice.Origin == i18n.FromFlag {
				fmt.Fprint(cmd.ErrOrStderr(), nt.T("SKILUS_LANG sigue mandando mientras esté definida.\n"))
			}
			return nil
		},
	}
	cmd.AddCommand(set)
	return cmd
}

func renderLanguage(w io.Writer, choice i18n.Choice, t *i18n.Catalog) {
	l, _ := i18n.Lookup(choice.Code)
	var why string
	switch choice.Origin {
	case i18n.FromFlag:
		why = t.T("por la variable SKILUS_LANG")
	case i18n.FromSetting:
		why = t.T("elegido con skilus lang set")
	case i18n.FromEnv:
		why = t.T("según %s", choice.Detail)
	case i18n.FromSystem:
		why = t.T("según el sistema (%s)", choice.Detail)
	default:
		why = t.T("por defecto: el sistema no indica un idioma disponible")
	}
	fmt.Fprint(w, t.T("Idioma: %s (%s), %s.\n", l.Name, l.Code, why))
	fmt.Fprintln(w, t.T("Disponibles:"))
	for _, x := range i18n.Languages {
		mark := " "
		if x.Code == choice.Code {
			mark = "*"
		}
		fmt.Fprintf(w, "  %s %s  %s\n", mark, x.Code, x.Name)
	}
	fmt.Fprint(w, t.T("Cámbialo con skilus lang set <idioma>; auto vuelve a seguir al sistema.\n"))
}

func languageCodes() string {
	codes := make([]string, len(i18n.Languages))
	for i, l := range i18n.Languages {
		codes[i] = l.Code
	}
	return strings.Join(codes, ", ")
}

// localizeCobra translates the texts Cobra adds by itself: the usage
// template, the help command and the --help and --version flags.
func localizeCobra(root *cobra.Command, t *i18n.Catalog) {
	root.SetUsageTemplate(usageTemplate(t))
	root.InitDefaultHelpCmd()
	for _, c := range root.Commands() {
		if c.Name() == "help" {
			c.Short = t.T("Ayuda sobre cualquier comando")
			c.Long = t.T("Muestra la ayuda de cualquier comando de skilus.")
		}
	}
	if root.Version != "" {
		root.Flags().BoolP("version", "v", false, t.T("muestra la versión de skilus"))
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("help") == nil {
			c.Flags().BoolP("help", "h", false, t.T("ayuda de %s", c.Name()))
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// usageTemplate is Cobra's default usage template with its headings
// translated.
func usageTemplate(t *i18n.Catalog) string {
	r := strings.NewReplacer(
		"«Uso:»", t.T("Uso:"),
		"«Alias:»", t.T("Alias:"),
		"«Ejemplos:»", t.T("Ejemplos:"),
		"«Comandos:»", t.T("Comandos:"),
		"«Opciones:»", t.T("Opciones:"),
		"«Opciones globales:»", t.T("Opciones globales:"),
		"«Más ayuda:»", t.T("Más temas de ayuda:"),
		"«comando»", t.T("comando"),
		"«Usa…»", t.T(`Usa "%s [comando] --help" para ver la ayuda de un comando.`, "{{.CommandPath}}"),
	)
	return r.Replace(`«Uso:»{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [«comando»]{{end}}{{if gt (len .Aliases) 0}}

«Alias:»
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

«Ejemplos:»
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

«Comandos:»{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

«Opciones:»
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

«Opciones globales:»
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

«Más ayuda:»{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

«Usa…»{{end}}
`)
}
