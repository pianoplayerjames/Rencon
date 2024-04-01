#!/bin/bash

# Define the project directory as the current directory
PROJECT_DIR="."

# Define the GitHub repository URL
REPO_URL="https://github.com/pianoplayerjames/rencoin.git"

# Navigate to the project directory
cd "$PROJECT_DIR"

# Check if the directory exists and is a git repository
if [ -d ".git" ]; then
    echo "Updating the existing repository"
    git pull
else
    echo "Project directory is not a git repository. Attempting to clone."
    # Optional: Backup the existing directory if needed
    # Since we are operating in the current directory, be careful with backups to not overwrite.
    # mv "$PROJECT_DIR" "${PROJECT_DIR}_backup_$(date +%Y%m%d%H%M%S)"
    # Clone the repository afresh
    git clone "$REPO_URL" .
fi

# Optional: Commands to handle dependencies or configurations
# npm install
# cp ".env.example" ".env"

echo "Update completed."
