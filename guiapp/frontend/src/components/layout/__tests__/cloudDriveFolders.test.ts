import { describe, expect, it } from 'vitest';
import { classifyCloudDriveItem, cloudDriveItemMatchesQuery, groupCloudDrive, type CloudDriveItem } from '../cloudDriveFolders';

function item(partial: CloudDriveItem & { id: string }): CloudDriveItem {
  return partial;
}

describe('classifyCloudDriveItem', () => {
  it('keeps meeting recordings in Audio even when the filename looks like video', () => {
    expect(classifyCloudDriveItem(item({
      id: 'rec',
      type: 'audio',
      source_filename: 'meeting.mp4',
      source_content_type: 'audio/mp4',
    }))).toEqual({ folder: 'audio' });
  });

  it('splits documents by file type and leaves media in the other folders', () => {
    expect(classifyCloudDriveItem(item({ id: '1', source_filename: 'spec.pdf' })).category).toBe('pdf');
    expect(classifyCloudDriveItem(item({ id: '2', source_filename: 'note.docx' })).category).toBe('word');
    expect(classifyCloudDriveItem(item({ id: '3', source_filename: 'sheet.xlsx' })).category).toBe('sheet');
    expect(classifyCloudDriveItem(item({ id: '4', source_filename: 'deck.pptx' })).category).toBe('slides');
    expect(classifyCloudDriveItem(item({ id: '5', source_filename: 'readme.md' })).category).toBe('markdown');
    expect(classifyCloudDriveItem(item({ id: '6', source_filename: 'notes.txt' })).category).toBe('text');
    expect(classifyCloudDriveItem(item({ id: '7', source_filename: 'page.html' })).category).toBe('web');
    expect(classifyCloudDriveItem(item({ id: '8', source_filename: 'paper.tex' })).category).toBe('latex');
    expect(classifyCloudDriveItem(item({ id: '9', source_filename: 'clip.mp4' })).folder).toBe('video');
    expect(classifyCloudDriveItem(item({ id: '10', source_filename: 'song.mp3' })).folder).toBe('audio');
    expect(classifyCloudDriveItem(item({ id: '11', source_filename: 'photo.png' })).folder).toBe('other');
    expect(classifyCloudDriveItem(item({ id: '12', source_filename: 'bundle.zip' })).folder).toBe('other');
    expect(classifyCloudDriveItem(item({ id: '13', source_filename: 'main.go' })).folder).toBe('other');
  });

  it('treats extensionless notes as text documents and trusts MIME when the name has no suffix', () => {
    expect(classifyCloudDriveItem(item({ id: 'minutes', title: '周会纪要' }))).toEqual({
      folder: 'documents',
      category: 'text',
    });
    expect(classifyCloudDriveItem(item({
      id: 'bare-sheet',
      title: '导出',
      source_content_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    })).category).toBe('sheet');
  });

  it('does not treat a dotted title or a nameless binary as a text document', () => {
    expect(classifyCloudDriveItem(item({ id: 'dated', title: '2026.10.05 纪要' }))).toEqual({
      folder: 'documents',
      category: 'text',
    });
    expect(classifyCloudDriveItem(item({
      id: 'blob',
      title: '上传文件',
      source_content_type: 'application/octet-stream',
    })).folder).toBe('other');
    expect(classifyCloudDriveItem(item({
      id: 'json',
      source_filename: 'data.json',
      source_content_type: 'text/plain',
    })).folder).toBe('other');
    expect(classifyCloudDriveItem(item({
      id: 'named-pdf',
      source_filename: 'blob.bin',
      source_content_type: 'application/pdf',
    })).category).toBe('pdf');
    expect(classifyCloudDriveItem(item({ id: 'wps', source_filename: '合同.wps' })).category).toBe('word');
    expect(classifyCloudDriveItem(item({ id: 'et', source_filename: '表.et' })).category).toBe('sheet');
    expect(classifyCloudDriveItem(item({ id: 'dps', source_filename: '稿.dps' })).category).toBe('slides');
  });

  it('prefers a spreadsheet extension over a generic text MIME type', () => {
    expect(classifyCloudDriveItem(item({
      id: 'csv',
      source_filename: 'data.csv',
      source_content_type: 'text/plain',
    })).category).toBe('sheet');
  });
});

describe('groupCloudDrive', () => {
  it('always returns the four folders and nests documents by type', () => {
    const groups = groupCloudDrive([
      item({ id: 'a', source_filename: 'a.pdf' }),
      item({ id: 'b', source_filename: 'b.docx' }),
      item({ id: 'c', source_filename: 'c.pdf' }),
      item({ id: 'd', type: 'audio', title: '录音' }),
      item({ id: 'e', source_filename: 'clip.mov' }),
      item({ id: 'f', source_filename: 'pic.jpg' }),
    ]);

    expect(groups.map((group) => group.folder)).toEqual(['documents', 'audio', 'video', 'other']);
    expect(groups[0].items.map((row) => row.id)).toEqual(['a', 'b', 'c']);
    expect(groups[0].categories.map((category) => [category.category, category.items.map((row) => row.id)])).toEqual([
      ['pdf', ['a', 'c']],
      ['word', ['b']],
    ]);
    expect(groups[1].items.map((row) => row.id)).toEqual(['d']);
    expect(groups[1].categories).toEqual([]);
    expect(groups[2].items.map((row) => row.id)).toEqual(['e']);
    expect(groups[3].items.map((row) => row.id)).toEqual(['f']);
  });

  it('matches a type name without letting a single letter select the whole folder', () => {
    const sheet = item({ id: 'sheet', title: '预算', source_filename: '预算.xlsx' });
    const note = item({ id: 'note', title: '周会纪要' });
    expect(cloudDriveItemMatchesQuery(sheet, '表格', ['Spreadsheets', '表格'])).toBe(true);
    expect(cloudDriveItemMatchesQuery(sheet, '预算', ['Spreadsheets', '表格'])).toBe(true);
    expect(cloudDriveItemMatchesQuery(note, 'a', ['Documents', '文档', 'Text', '文本'])).toBe(false);
    expect(cloudDriveItemMatchesQuery(note, 'do', ['Documents', '文档', 'Text', '文本'])).toBe(false);
    expect(cloudDriveItemMatchesQuery(note, 'documents', ['Documents', '文档', 'Text', '文本'])).toBe(true);
    expect(cloudDriveItemMatchesQuery(sheet, '表', ['Spreadsheets', '表格'])).toBe(true);
    expect(cloudDriveItemMatchesQuery(
      item({ id: 'pdf', title: '规格', source_filename: 'spec.pdf' }),
      '文',
      ['Documents', '文档', 'PDF'],
      ['PDF'],
    )).toBe(false);
    expect(cloudDriveItemMatchesQuery(note, '文本', ['Documents', '文档', 'Text', '文本'])).toBe(true);
  });

  it('keeps empty folders so the drive still shows its default structure', () => {
    const groups = groupCloudDrive([]);
    expect(groups.map((group) => [group.folder, group.items.length, group.categories.length])).toEqual([
      ['documents', 0, 0],
      ['audio', 0, 0],
      ['video', 0, 0],
      ['other', 0, 0],
    ]);
  });
});
