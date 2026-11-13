import { test, expect } from '../../fixtures/test-fixtures';
import { createTempFile, generateTestImage } from '../../helpers/test-data';
import { selectors } from '../../helpers/selectors';
import * as path from 'path';

// The image editor opened on Brush, dark grey, 8px every time. It now comes
// back with the drawing tool, colour, opacity and size last used.
test.describe('Image editor remembers its tool settings', () => {
  const activeTool = (app: any) => app.page.locator('.editor-tool-btn.active');

  test('tool, colour and size survive closing and reopening', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openImageEditor(filename);
    await expect(activeTool(app)).toHaveAttribute('data-tool', 'brush');
    await expect(app.page.locator(selectors.editor.colorPicker)).toHaveValue('#44403c');

    await app.selectTool('arrow');
    await app.setEditorColor('#ff0000');
    await app.setBrushSize(20);
    await app.closeImageEditor();

    await app.openImageEditor(filename);
    await expect(activeTool(app)).toHaveAttribute('data-tool', 'arrow');
    await expect(app.page.locator(selectors.editor.colorPicker)).toHaveValue('#ff0000');
    await expect(app.page.locator(selectors.editor.brushSize)).toHaveValue('20');
    expect(await app.page.evaluate(() => ({
      // @ts-ignore
      color: EditorCore.currentColor, size: EditorCore.brushSize, tool: EditorCore.activeToolName,
    }))).toEqual({ color: '#ff0000', size: 20, tool: 'arrow' });
    await app.closeImageEditor();
  });

  test('a one-off tool is not reopened, but its colour and size are kept', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);

    await app.openImageEditor(filename);
    await app.selectTool('rectangle');
    await app.setEditorColor('#00ff00');
    await app.selectTool('eyedropper');
    await app.closeImageEditor();

    await app.openImageEditor(filename);
    await expect(activeTool(app)).toHaveAttribute('data-tool', 'rectangle');
    await expect(app.page.locator(selectors.editor.colorPicker)).toHaveValue('#00ff00');
    await app.closeImageEditor();
  });

  test('a corrupt stored value falls back to the defaults', async ({ app }) => {
    const imagePath = await createTempFile(generateTestImage(), 'png');
    const filename = path.basename(imagePath);
    await app.uploadFile(imagePath);
    await app.page.evaluate(() => localStorage.setItem('mahpastes.editor.toolPrefs',
      JSON.stringify({ tool: 'crop', color: 'red;background:url(x)', opacity: 7, brushSize: 9999, fontSize: 'big' })));

    await app.openImageEditor(filename);
    await expect(activeTool(app)).toHaveAttribute('data-tool', 'brush');
    await expect(app.page.locator(selectors.editor.colorPicker)).toHaveValue('#44403c');
    await expect(app.page.locator(selectors.editor.brushSize)).toHaveValue('8');
    await app.closeImageEditor();
  });
});
