#!/bin/bash
set -e

# Define variables
PROJECT_ROOT=$(dirname "$(dirname "$(readlink -f "$0")")")
TARGET_NAME="picoclaw260301"
TARGET_USERS=("beichen" "zhibing")

echo "Building picoclaw..."
cd "$PROJECT_ROOT"
# Use domestic Go proxy
export GOPROXY=https://goproxy.cn,direct
if go build -o picoclaw ./cmd/picoclaw; then
    echo "Build successful."
else
    echo "Build failed!"
    exit 1
fi

for user in "${TARGET_USERS[@]}"; do
    TARGET_DIR="/home/$user"
    TARGET_FILE="$TARGET_DIR/$TARGET_NAME"
    
    echo "--------------------------------------------------"
    echo "Deploying to $user ($TARGET_FILE)..."

    # Check if target directory exists
    if [ ! -d "$TARGET_DIR" ]; then
        echo "Warning: Directory $TARGET_DIR does not exist. Skipping."
        continue
    fi

    # Check if we can write to target directory
    if [ -w "$TARGET_DIR" ]; then
        rm -f "$TARGET_FILE"
        cp picoclaw "$TARGET_FILE"
    else
        echo "Target directory is not writable, using sudo..."
        sudo rm -f "$TARGET_FILE"
        sudo cp picoclaw "$TARGET_FILE"
    fi

    echo "Verifying deployment for $user..."
    if [ -r "$TARGET_FILE" ]; then
        ls -l --time-style=long-iso "$TARGET_FILE"
    else
        echo "Using sudo to verify..."
        sudo ls -l --time-style=long-iso "$TARGET_FILE"
    fi
done

echo "--------------------------------------------------"
echo "All deployments done."
