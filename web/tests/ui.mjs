export async function choose(page, label, value) {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('listbox', { name: label, exact: true }).locator('[role="option"][data-value="' + value + '"]').click()
}

export async function dismissGuide(context, origin = 'http://127.0.0.1:5897') {
  const response = await context.request.post(origin + '/api/v1/profile/onboarding', { headers: { 'Content-Type': 'application/json', 'X-MiyoHub-Request': '1' }, data: { status: 'dismissed' } })
  if (!response.ok()) throw new Error('Could not dismiss fixture onboarding')
}

export async function editCaptcha(page, panel, number, edit) {
  await panel.getByRole('button', { name: '配置验证码渠道 ' + number, exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '配置验证码渠道', exact: true })
  await edit(dialog)
  await dialog.getByRole('button', { name: '应用渠道配置', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
}

export async function addCaptcha(page, panel, provider, edit) {
  await panel.getByRole('button', { name: '添加验证码渠道', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '添加验证码渠道', exact: true })
  if (provider !== 'custom') await choose(page, '验证码渠道类型', provider)
  await edit(dialog)
  await dialog.getByRole('button', { name: '应用渠道配置', exact: true }).click()
  await dialog.waitFor({ state: 'hidden' })
}
