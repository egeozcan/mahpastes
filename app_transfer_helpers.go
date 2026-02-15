package main

import (
	"errors"
	"fmt"
)

func (a *App) prepareClipTransferItem(clipID int64, channel string) (*PreparedTransferItem, error) {
	if a.tempStore == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	prepared, err := a.tempStore.PrepareClipFile(clipID)
	if err != nil {
		if errors.Is(err, ErrClipNotFound) {
			return nil, fmt.Errorf("clip not found")
		}
		return nil, err
	}

	_ = channel // channel is reserved for future transfer-channel-specific policies

	return &PreparedTransferItem{
		ClipID:         prepared.ClipID,
		AbsPath:        prepared.AbsPath,
		FileURL:        fileURLFromAbsPath(prepared.AbsPath),
		Filename:       prepared.Filename,
		ContentType:    prepared.ContentType,
		LeaseExpiresAt: prepared.LeaseExpiresAt,
	}, nil
}

func (a *App) lookupPreparedClipTransferItem(clipID int64, channel string) (*PreparedTransferItem, error) {
	if a.tempStore == nil {
		return nil, fmt.Errorf("temp clip store is not initialized")
	}
	prepared, err := a.tempStore.FindExistingClipFile(clipID)
	if err != nil {
		if errors.Is(err, ErrClipNotFound) {
			return nil, fmt.Errorf("clip not found")
		}
		return nil, err
	}
	if prepared == nil {
		return nil, nil
	}

	_ = channel // channel is reserved for future transfer-channel-specific policies

	return &PreparedTransferItem{
		ClipID:         prepared.ClipID,
		AbsPath:        prepared.AbsPath,
		FileURL:        fileURLFromAbsPath(prepared.AbsPath),
		Filename:       prepared.Filename,
		ContentType:    prepared.ContentType,
		LeaseExpiresAt: prepared.LeaseExpiresAt,
	}, nil
}

func (a *App) deleteTempFilesForClipIDs(ids []int64) error {
	if a.tempStore == nil {
		return nil
	}
	return a.tempStore.DeleteForClipIDs(ids)
}
