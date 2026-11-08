package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"chat-online/internal/auth"
	"chat-online/internal/config"
	"chat-online/internal/database"
	"chat-online/internal/storage"
	"chat-online/internal/users"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao carregar configuração: %v\n", err)
		os.Exit(1)
	}

	pool, err := database.ConnectAndMigrate(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao conectar no banco: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	usersService := users.NewService(users.NewRepository(pool))

	switch subcmd {
	case "create":
		fs := flag.NewFlagSet("create", flag.ExitOnError)
		username := fs.String("username", "", "Identificador ou e-mail do usuário")
		name := fs.String("name", "", "Nome de exibição")
		password := fs.String("password", "", "Senha (mínimo 8 caracteres)")
		_ = fs.Parse(os.Args[2:])

		if *username == "" || *name == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "uso: go run ./cmd/userctl create -username <id> -name <nome> -password <senha>")
			os.Exit(1)
		}
		if err := auth.ValidatePasswordStrength(*password); err != nil {
			fmt.Fprintf(os.Stderr, "senha inválida: %v\n", err)
			os.Exit(1)
		}
		hash, err := auth.HashPassword(*password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao gerar hash: %v\n", err)
			os.Exit(1)
		}
		created, err := usersService.CreateUser(ctx, *username, *name, hash, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao criar usuário: %v\n", err)
			os.Exit(1)
		}
		out, _ := json.MarshalIndent(created.User, "", "  ")
		fmt.Println(string(out))

	case "requests":
		requests, err := usersService.ListAccountRequests(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao listar solicitações: %v\n", err)
			os.Exit(1)
		}
		out, _ := json.MarshalIndent(requests, "", "  ")
		fmt.Println(string(out))

	case "lgpd-export":
		fs := flag.NewFlagSet("lgpd-export", flag.ExitOnError)
		userID := fs.String("user-id", "", "UUID do usuário")
		_ = fs.Parse(os.Args[2:])
		if *userID == "" {
			fmt.Fprintln(os.Stderr, "uso: go run ./cmd/userctl lgpd-export -user-id <uuid>")
			os.Exit(1)
		}
		exported, err := usersService.ExportUserData(ctx, *userID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao exportar dados: %v\n", err)
			os.Exit(1)
		}
		out, _ := json.MarshalIndent(exported, "", "  ")
		fmt.Println(string(out))

	case "lgpd-delete":
		fs := flag.NewFlagSet("lgpd-delete", flag.ExitOnError)
		userID := fs.String("user-id", "", "UUID do usuário")
		_ = fs.Parse(os.Args[2:])
		if *userID == "" {
			fmt.Fprintln(os.Stderr, "uso: go run ./cmd/userctl lgpd-delete -user-id <uuid>")
			os.Exit(1)
		}
		store, err := storage.NewLocalStorage(cfg.UploadStorageDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao inicializar storage: %v\n", err)
			os.Exit(1)
		}
		keys, err := usersService.AnonymizeAndDeleteUser(ctx, *userID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "erro ao excluir conta: %v\n", err)
			os.Exit(1)
		}
		for _, k := range keys {
			_ = store.Delete(ctx, k)
		}
		fmt.Printf("Conta %s anonimizada, sessões revogadas e %d arquivo(s) removido(s).\n", *userID, len(keys))

	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Comandos disponíveis em userctl:")
	fmt.Fprintln(os.Stderr, "  create       -username <login> -name <nome> -password <senha>")
	fmt.Fprintln(os.Stderr, "  requests     (lista solicitações de novas contas feitas pelos usuários)")
	fmt.Fprintln(os.Stderr, "  lgpd-export  -user-id <uuid>")
	fmt.Fprintln(os.Stderr, "  lgpd-delete  -user-id <uuid>")
}

