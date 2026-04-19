import { test } from '../../fixtures/test-fixtures';
import { expect } from '@playwright/test';

// Playwright in this repo reuses a worker-scoped page between tests and
// fastReset() doesn't clean up ad hoc DOM nodes, so any input/dropdown we
// mount must be torn down explicitly or the next test can observe stale
// rows. Each test uses a uniquely-ID'd container + per-input scoped locators.

const MOUNT_CONTAINER_PREFIX = 'ac-test-container-';

test.afterEach(async ({ app }) => {
    await app.page.evaluate((prefix) => {
        document.querySelectorAll(`[id^="${prefix}"]`).forEach(el => el.remove());
    }, MOUNT_CONTAINER_PREFIX);
});

test.describe('TagAutocomplete allowCreate option', () => {
    test('does not offer "Create new" row when allowCreate is false', async ({ app }) => {
        await app.createTag('existing-tag');

        const containerID = `${MOUNT_CONTAINER_PREFIX}nocreate`;
        const inputID = `${containerID}-input`;

        await app.page.evaluate(([cID, iID]) => {
            const box = document.createElement('div');
            box.id = cID;
            box.style.position = 'relative';
            const input = document.createElement('input');
            input.id = iID;
            box.appendChild(input);
            document.body.appendChild(box);
            // @ts-expect-error — runtime helper
            const handle = window.TagAutocomplete.attach(input, { allowCreate: false });
            // @ts-expect-error
            window[`__ac_handle_${cID}`] = handle;
        }, [containerID, inputID]);

        const input = app.page.locator(`#${inputID}`);
        await input.fill('brand-new-never-created');
        await app.page.waitForTimeout(100);

        // Scope the locator to THIS input's dropdown only (the immediate next
        // sibling — TagAutocomplete uses input.insertAdjacentElement('afterend', dropdown)).
        const scopedCreateRow = app.page.locator(
            `#${inputID} + div [role="option"]:has-text("Create new")`,
        );
        await expect(scopedCreateRow).toHaveCount(0);

        await app.page.evaluate((cID) => {
            // @ts-expect-error
            const h = window[`__ac_handle_${cID}`];
            if (h && typeof h.destroy === 'function') h.destroy();
            // @ts-expect-error
            delete window[`__ac_handle_${cID}`];
        }, containerID);
    });

    test('still offers "Create new" row by default (backward compat)', async ({ app }) => {
        const containerID = `${MOUNT_CONTAINER_PREFIX}default`;
        const inputID = `${containerID}-input`;

        await app.page.evaluate(([cID, iID]) => {
            const box = document.createElement('div');
            box.id = cID;
            box.style.position = 'relative';
            const input = document.createElement('input');
            input.id = iID;
            box.appendChild(input);
            document.body.appendChild(box);
            // @ts-expect-error
            const handle = window.TagAutocomplete.attach(input, {});
            // @ts-expect-error
            window[`__ac_handle_${cID}`] = handle;
        }, [containerID, inputID]);

        const input = app.page.locator(`#${inputID}`);
        await input.fill('another-new-tag');
        await app.page.waitForTimeout(100);

        const scopedCreateRow = app.page.locator(
            `#${inputID} + div [role="option"]:has-text("Create new")`,
        );
        await expect(scopedCreateRow).toHaveCount(1);

        await app.page.evaluate((cID) => {
            // @ts-expect-error
            const h = window[`__ac_handle_${cID}`];
            if (h && typeof h.destroy === 'function') h.destroy();
            // @ts-expect-error
            delete window[`__ac_handle_${cID}`];
        }, containerID);
    });
});
