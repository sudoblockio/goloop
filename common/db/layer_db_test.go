/*
 * Copyright 2023 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testOperation struct {
	bk BucketID
	key, value []byte
}

type testBucket struct {
	Bucket
	id    BucketID
	dbase *testDatabase
}

func (bk *testBucket) Set(key []byte, value []byte) error {
	bk.dbase.writeOperation(testOperation{bk.id, key, value})
	return nil
}

func (bk *testBucket) Delete(key []byte) error {
	bk.dbase.writeOperation(testOperation{bk.id, key, nil})
	return nil
}

type testDatabase struct {
	Database
	buckets map[string]*testBucket
	record  []testOperation
}

func (t *testDatabase) writeOperation(op testOperation) {
	t.record = append(t.record, op)
}

func (t *testDatabase) GetBucket(id BucketID) (Bucket, error) {
	if bk, ok := t.buckets[string(id)] ; ok {
		return bk, nil
	}
	bk := &testBucket{id: id, dbase: t}
	t.buckets[string(id)] = bk
	return bk, nil
}

func newTestDatabase() *testDatabase {
	return &testDatabase{
		buckets: make(map[string]*testBucket),
	}
}

func (t *testDatabase) Close() error {
	return nil
}

func TestLayerDB_FlushInOrder(t *testing.T) {
	scenario := []testOperation{
		{ BytesByHash, []byte("key1"), []byte("value1") },
		{ BytesByHash, []byte("key1"), nil },
		{ BytesByHash, []byte("key2"), []byte("value2") },
		{ BytesByHash, []byte("key3"), []byte("value3") },
		{ BytesByHash, []byte("key2"), []byte("value4") },
		{ ChainProperty, []byte("key4"), []byte("value4") },
		{ BytesByHash, []byte("key5"), []byte("value5") },
		{ ChainProperty, []byte("key4"), []byte("valueX") },
	}

	exp := []testOperation{
		{ BytesByHash, []byte("key1"), nil },
		{ BytesByHash, []byte("key3"), []byte("value3") },
		{ BytesByHash, []byte("key2"), []byte("value4") },
		{ BytesByHash, []byte("key5"), []byte("value5") },
		{ ChainProperty, []byte("key4"), []byte("valueX") },
	}

	dbase := newTestDatabase()
	ldb := NewLayerDB(dbase)

	for _, c := range scenario {
		bk, err := ldb.GetBucket(c.bk)
		assert.NoError(t, err)
		assert.NotNil(t, bk)

		if c.value == nil {
			err := bk.Delete(c.key)
			assert.NoError(t, err)
		} else {
			err := bk.Set(c.key, c.value)
			assert.NoError(t, err)
		}
	}

	err := ldb.Flush(true)
	assert.NoError(t, err)

	// check records
	assert.NoError(t, err)
	assert.Equal(t, exp, dbase.record)

	assert.Equal(t, Unwrap(ldb), dbase)
}