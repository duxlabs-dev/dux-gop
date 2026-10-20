package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/fsnotify/fsnotify"
)

type ColorManager struct {
	mu       sync.RWMutex
	palette  *models.ColorPalette
	watcher  *fsnotify.Watcher
	filePath string
	notify   chan struct{}
}

func NewColorManager() (*ColorManager, error) {
	configDir, err := appPaths.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	filePath := filepath.Join(configDir, "colors.json")

	cm := &ColorManager{
		palette:  models.DefaultColorPalette(),
		filePath: filePath,
		notify:   make(chan struct{}, 1),
	}

	if err := cm.loadOrCreateConfigFile(); err != nil {
		return nil, fmt.Errorf("failed to initialize config file: %w", err)
	}

	if err := cm.startWatching(); err != nil {
		return nil, fmt.Errorf("failed to start file watching: %w", err)
	}

	return cm, nil
}

func (cm *ColorManager) GetPalette() *models.ColorPalette {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	paletteCopy := *cm.palette
	return &paletteCopy
}

func (cm *ColorManager) Close() error {
	if cm.watcher != nil {
		return cm.watcher.Close()
	}
	return nil
}

func (cm *ColorManager) ColorChanges() <-chan struct{} {
	return cm.notify
}

func (cm *ColorManager) notifyColorChange() {
	select {
	case cm.notify <- struct{}{}:
	default:
	}
}

func (cm *ColorManager) loadOrCreateConfigFile() error {
	if _, err := os.Stat(cm.filePath); os.IsNotExist(err) {
		return cm.createDefaultConfigFile()
	}

	return cm.loadConfigFile()
}

func (cm *ColorManager) createDefaultConfigFile() error {
	data, err := json.MarshalIndent(cm.palette, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal default palette: %w", err)
	}

	if err := os.WriteFile(cm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func (cm *ColorManager) loadConfigFile() error {
	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var palette models.ColorPalette
	if err := json.Unmarshal(data, &palette); err != nil {
		return err
	}

	cm.mu.Lock()
	cm.palette = &palette
	cm.mu.Unlock()

	cm.notifyColorChange()
	return nil
}

func (cm *ColorManager) startWatching() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	cm.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) && event.Name == cm.filePath {
					if err := cm.loadConfigFile(); err != nil {
						cm.mu.Lock()
						cm.palette = models.DefaultColorPalette()
						cm.mu.Unlock()
						cm.notifyColorChange()
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				_ = err
			}
		}
	}()

	return watcher.Add(cm.filePath)
}
